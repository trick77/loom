package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/docgen"
	"github.com/trick77/slopr/internal/imagegen"
	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/sse"
)

func (s *server) executeToolCall(ctx context.Context, user auth.User, call llm.ToolCall, round int) string {
	args := summarizeForLog(call.Function.Arguments)
	arguments, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		slog.Warn("tool call rejected: invalid arguments", "tool", call.Function.Name, "round", round, "args", args, "err", err)
		return capToolOutput("tool failed: invalid arguments: " + err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, maxToolCallDuration)
	defer cancel()
	start := time.Now()
	output, err := s.mcp.CallTool(callCtx, call.Function.Name, arguments)
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		slog.Warn("tool call failed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "err", err)
		if fallback, ok := s.fetchObscuraFallback(callCtx, user, call.Function.Name, arguments, round); ok {
			return fallback
		}
		return capToolOutput("tool failed: " + err.Error())
	}
	slog.Info("tool call completed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "result_bytes", len(output))
	s.countToolCall(ctx, user, call.Function.Name)
	return capToolOutput(output)
}

// countToolCall increments the per-user counter for a successfully completed
// tool call. An obscura page load is counted per browser_navigate (one fetch =
// one navigated page); this covers the model driving obscura directly. The
// deterministic fetch->obscura fallback navigates obscura outside this path, so
// it counts itself in fetchObscuraFallback — there is no double count.
func (s *server) countToolCall(ctx context.Context, user auth.User, toolName string) {
	switch toolName {
	case tavilySearchExposedName:
		s.recordUsage("web_search", func() error { return s.usage.IncWebSearch(ctx, user.ID) })
	case fetchToolName:
		s.recordUsage("web_fetch", func() error { return s.usage.IncWebFetch(ctx, user.ID) })
	case obscuraNavigateToolName:
		s.recordUsage("obscura_fetch", func() error { return s.usage.IncObscuraFetch(ctx, user.ID) })
	}
}

// Tool names involved in the deterministic fetch->obscura fallback. fetch is the
// lightweight HTTP reader; when it fails on a URL we retry with obscura's
// headless browser (navigate, then snapshot the rendered page).
const (
	fetchToolName           = "fetch__fetch"
	obscuraNavigateToolName = "obscura__browser_navigate"
	obscuraSnapshotToolName = "obscura__browser_snapshot"
	// tavilySearchExposedName is the namespaced web-search tool as dispatched
	// (server "tavily" + tool "tavily_search"); see internal/mcp ExposedToolName.
	tavilySearchExposedName = "tavily__tavily_search"
)

// fetchObscuraFallback retries a failed fetch via obscura's headless browser.
// It only fires for the fetch tool when obscura is configured and the call
// carried a URL. On success it returns the obscura snapshot and true; otherwise
// it returns ok=false so the caller surfaces the original fetch failure.
func (s *server) fetchObscuraFallback(ctx context.Context, user auth.User, toolName string, arguments map[string]any, round int) (string, bool) {
	if toolName != fetchToolName {
		return "", false
	}
	if !s.mcp.HasTool(obscuraNavigateToolName) || !s.mcp.HasTool(obscuraSnapshotToolName) {
		return "", false
	}
	url, ok := arguments["url"].(string)
	if !ok || strings.TrimSpace(url) == "" {
		return "", false
	}
	if _, err := s.mcp.CallTool(ctx, obscuraNavigateToolName, map[string]any{"url": url}); err != nil {
		slog.Warn("obscura fallback navigate failed", "url", url, "round", round, "err", err)
		return "", false
	}
	snapshot, err := s.mcp.CallTool(ctx, obscuraSnapshotToolName, map[string]any{})
	if err != nil {
		slog.Warn("obscura fallback snapshot failed", "url", url, "round", round, "err", err)
		return "", false
	}
	slog.Info("fetch failed, obscura fallback succeeded", "url", url, "round", round, "result_bytes", len(snapshot))
	s.recordUsage("obscura_fetch", func() error { return s.usage.IncObscuraFetch(ctx, user.ID) })
	return capToolOutput(snapshot), true
}

func (s *server) availableTools() []llm.Tool {
	tools := []llm.Tool(nil)
	names := map[string]string{}
	if s.artifacts != nil && strings.TrimSpace(s.usersDir) != "" {
		for _, gen := range s.docTools {
			schema := gen.Schema()
			names[schema.Name] = "built_in"
			tools = append(tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        schema.Name,
					Description: schema.Description,
					Parameters:  schema.Parameters,
				},
			})
		}
		for _, gen := range s.imageTools {
			schema := gen.Schema()
			if owner, exists := names[schema.Name]; exists {
				slog.Warn("skipping duplicate image tool name", "tool", schema.Name, "existing", owner)
				continue
			}
			names[schema.Name] = "built_in_image"
			tools = append(tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        schema.Name,
					Description: schema.Description,
					Parameters:  schema.Parameters,
				},
			})
		}
	}
	if s.mcp != nil {
		for _, tool := range s.mcp.Tools() {
			if owner, exists := names[tool.Function.Name]; exists {
				slog.Warn("skipping duplicate MCP tool name", "tool", tool.Function.Name, "existing", owner)
				continue
			}
			names[tool.Function.Name] = "mcp"
			tools = append(tools, tool)
		}
	}
	return tools
}

func findGenerateImageTool(tools []llm.Tool) *llm.Tool {
	for _, tool := range tools {
		if tool.Function.Name == "generate_image" {
			selected := tool
			return &selected
		}
	}
	return nil
}

func (s *server) executeBuiltInTool(ctx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, call llm.ToolCall) (string, *artifactResponse, bool) {
	if response, output, handled := s.executeImageTool(ctx, stream, user, thread, call); handled {
		return output, response, true
	}
	generator := s.docGenerator(call.Function.Name)
	if generator == nil {
		return "", nil, false
	}
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return capToolOutput("tool failed: invalid arguments: " + err.Error()), nil, true
	}
	filename, _ := args["filename"].(string)
	var buffer bytes.Buffer
	meta, err := generator.Generate(docgen.GenerateRequest{
		Format:   generator.ToolName(),
		Filename: filename,
		Payload:  args,
	}, &buffer)
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil, true
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return "tool failed: generated file is too large", nil, true
	}
	out, file, err := artifact.CreateOutputFile(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
	})
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil, true
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: write artifact: " + err.Error()), nil, true
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: close artifact: " + err.Error()), nil, true
	}
	created, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        out.MIMEType,
		SizeBytes:       int64(buffer.Len()),
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: persist artifact: " + err.Error()), nil, true
	}
	response := artifactResponse{
		ID:              created.ID,
		DisplayFilename: created.DisplayFilename,
		MIMEType:        created.MIMEType,
		SizeBytes:       created.SizeBytes,
		ProjectID:       created.ProjectID,
		DownloadURL:     created.DownloadURL,
	}
	_ = sendSSEJSON(stream, "artifact", response)
	return fmt.Sprintf("created artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), &response, true
}

func (s *server) executeImageTool(ctx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, call llm.ToolCall) (*artifactResponse, string, bool) {
	generator := s.imageTool(call.Function.Name)
	if generator == nil {
		return nil, "", false
	}
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return nil, capToolOutput("tool failed: invalid arguments: " + err.Error()), true
	}
	req := imagegen.ToolRequest{}
	if prompt, _ := args["prompt"].(string); prompt != "" {
		req.Prompt = prompt
	}
	if filename, _ := args["filename"].(string); filename != "" {
		req.Filename = filename
	}
	if format, _ := args["output_format"].(string); format != "" {
		req.OutputFormat = format
	}
	if width, ok := numberArg(args["width"]); ok {
		req.Width = width
	}
	if height, ok := numberArg(args["height"]); ok {
		req.Height = height
	}
	if safety, ok := numberArg(args["safety_tolerance"]); ok {
		req.SafetyTolerance = &safety
	}
	if seed, ok := int64Arg(args["seed"]); ok {
		req.Seed = &seed
	}
	var buffer bytes.Buffer
	meta, err := generator.Generate(ctx, req, &buffer)
	if err != nil {
		output := capToolOutput("tool failed: " + err.Error())
		slog.Warn("image tool failed",
			"tool", call.Function.Name,
			"thread_id", thread.ID,
			"provider_error", err)
		return nil, output, true
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return nil, "tool failed: generated image is too large", true
	}
	out, file, err := artifact.CreateOutputFile(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
	})
	if err != nil {
		return nil, capToolOutput("tool failed: " + err.Error()), true
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		_ = os.Remove(out.AbsPath)
		return nil, capToolOutput("tool failed: write artifact: " + err.Error()), true
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(out.AbsPath)
		return nil, capToolOutput("tool failed: close artifact: " + err.Error()), true
	}
	created, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        meta.MIMEType,
		SizeBytes:       int64(buffer.Len()),
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		return nil, capToolOutput("tool failed: persist artifact: " + err.Error()), true
	}
	response := artifactResponse{
		ID:              created.ID,
		DisplayFilename: created.DisplayFilename,
		MIMEType:        created.MIMEType,
		SizeBytes:       created.SizeBytes,
		ProjectID:       created.ProjectID,
		DownloadURL:     created.DownloadURL,
		Model:           meta.Model,
		Provider:        meta.Provider,
		Width:           meta.Width,
		Height:          meta.Height,
		DurationMs:      meta.DurationMs,
	}
	s.recordUsage("image_gen", func() error { return s.usage.IncImageGen(ctx, user.ID) })
	_ = sendSSEJSON(stream, "artifact", response)
	return &response, fmt.Sprintf("created image artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), true
}

func (s *server) docGenerator(name string) docgen.Generator {
	for _, candidate := range s.docTools {
		if candidate.ToolName() == name {
			return candidate
		}
	}
	return nil
}

func (s *server) imageTool(name string) *imagegen.Tool {
	for i := range s.imageTools {
		if s.imageTools[i].ToolName() == name {
			return &s.imageTools[i]
		}
	}
	return nil
}

func numberArg(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

func int64Arg(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

func capToolOutput(output string) string {
	if len(output) <= maxToolResultContentBytes {
		return output
	}
	return output[:maxToolResultContentBytes]
}

// summarizeForLog trims a value (e.g. tool arguments) to a length that is safe
// to log: enough to debug, short enough not to flood the logs.
func summarizeForLog(value string) string {
	const maxLen = 256
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "…"
}

func parseToolArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}
