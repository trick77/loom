package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/inference"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/usage"
)

// toolRun is the outcome of an MCP tool call's network half: everything
// finishToolCall needs to account for the call and label its result.
type toolRun struct {
	arguments  map[string]any
	argsErr    error
	output     string
	err        error
	durationMS int64
}

// runToolCall performs the MCP call itself. It touches no per-turn state, so a
// round's independent web reads can run it concurrently (see startToolRuns).
func (s *Engine) runToolCall(ctx context.Context, call llm.ToolCall) toolRun {
	arguments, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return toolRun{argsErr: err}
	}
	// Ask Tavily for each result's favicon regardless of what the model requested,
	// so the sources sidebar can show real icons. Harmless if the model already set it.
	if call.Function.Name == tavilySearchExposedName {
		arguments["include_favicon"] = true
	}
	callCtx, cancel := context.WithTimeout(ctx, maxToolCallDuration)
	defer cancel()
	start := time.Now()
	output, err := s.mcp.CallTool(callCtx, call.Function.Name, arguments)
	return toolRun{arguments: arguments, output: output, err: err, durationMS: time.Since(start).Milliseconds()}
}

// finishToolCall turns a finished run into the tool result the model sees:
// the obscura fallback for a failed fetch, the usage count, and the [n] source
// labels. It writes to the source registry and the shared obscura browser, so
// it runs one call at a time, in the order the model issued the calls.
func (t *Run) finishToolCall(ctx context.Context, call llm.ToolCall, round int, reg *webSourceRegistry, run toolRun) string {
	args := summarizeForLog(call.Function.Arguments)
	if run.argsErr != nil {
		slog.Warn("tool call rejected: invalid arguments", "tool", call.Function.Name, "round", round, "args", args, "err", run.argsErr)
		return capToolOutput("tool failed: invalid arguments: " + run.argsErr.Error())
	}
	arguments, output, err, durationMS := run.arguments, run.output, run.err, run.durationMS
	if err != nil {
		slog.Warn("tool call failed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "err", err)
		// A fresh budget: callCtx is already expired when fetch failed on its deadline.
		fallbackCtx, cancelFallback := context.WithTimeout(ctx, maxToolCallDuration)
		defer cancelFallback()
		// A failed PDF extraction is reported as is: obscura on a PDF URL only
		// snapshots the browser's PDF viewer.
		if !mcp.IsPDFExtractionError(err) {
			if fallback, ok := t.fetchObscuraFallback(fallbackCtx, call.Function.Name, arguments, round, reg); ok {
				return fallback
			}
		}
		return capToolOutput("tool failed: " + err.Error())
	}
	slog.Info("tool call completed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "result_bytes", len(output))
	t.e.countToolCall(ctx, t.user, call.Function.Name)
	// Annotate web-search/fetch results with [n] citation markers and register
	// their source URLs before capping, so the model can cite them inline.
	return capToolOutput(t.e.relabelWebToolOutput(call.Function.Name, arguments, output, reg))
}

// concurrentToolRuns bounds how many of a round's web reads are in flight at
// once: enough to turn a dozen pasted links from a serial wait into a short
// one, small enough not to hammer a single host.
const concurrentToolRuns = 4

// runsConcurrently reports whether a tool is a stateless web read whose calls
// are independent of one another. Everything else keeps its place in the
// round's sequence: obscura drives one shared browser, the built-in tools
// write per-turn state.
func runsConcurrently(name string) bool {
	return toolPolicy(name).concurrent
}

// startToolRuns starts the network half of every call in the round that may
// run concurrently and is not skipped, and returns one channel per call (nil
// for a call that runs in sequence). The caller still consumes the runs in
// call order, so results, source numbering and history keep the model's order;
// only the waiting overlaps. A lone eligible call gains nothing and is left to
// the sequential path.
func (s *Engine) startToolRuns(ctx context.Context, calls []llm.ToolCall, skipped []bool) []<-chan toolRun {
	runs := make([]<-chan toolRun, len(calls))
	eligible := 0
	for i, call := range calls {
		if !skipped[i] && runsConcurrently(call.Function.Name) {
			eligible++
		}
	}
	if eligible < 2 {
		return runs
	}
	slots := make(chan struct{}, concurrentToolRuns)
	for i, call := range calls {
		if skipped[i] || !runsConcurrently(call.Function.Name) {
			continue
		}
		done := make(chan toolRun, 1)
		runs[i] = done
		go func() {
			// A panic here would otherwise take the process down: this goroutine
			// is outside the request's recovery middleware.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("tool call panicked", "tool", call.Function.Name, "panic", r)
					done <- toolRun{err: fmt.Errorf("tool call panicked: %v", r)}
				}
			}()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				done <- toolRun{err: context.Cause(ctx)}
				return
			}
			done <- s.runToolCall(ctx, call)
		}()
	}
	return runs
}

// countToolCall increments the per-user counter for a successfully completed
// MCP tool call (see toolSpec.counts). An obscura page load is counted per
// browser_navigate (one fetch = one navigated page); this covers the model
// driving obscura directly. The deterministic fetch->obscura fallback navigates
// obscura outside this path, so it counts itself in fetchObscuraFallback —
// there is no double count.
func (s *Engine) countToolCall(ctx context.Context, user auth.User, toolName string) {
	counter := toolPolicy(toolName).counts
	if counter == nil {
		return
	}
	usage.Record(s.usage, counter.label, func() error { return counter.inc(s.usage, ctx, user.ID) })
}

// Tool names involved in the deterministic fetch->obscura fallback. fetch is the
// lightweight HTTP reader; when it fails on a URL we retry with obscura's
// headless browser (navigate, then snapshot the rendered page).
const (
	fetchToolName           = "fetch__fetch"
	obscuraNavigateToolName = "obscura__browser_navigate"
	obscuraSnapshotToolName = "obscura__browser_snapshot"
	// tavilyServerName is the map key under which the built-in Tavily web-search
	// server is registered (see cmd/loom/main.go).
	tavilyServerName = "tavily"
)

// tavilySearchExposedName is the namespaced web-search tool as dispatched. It is
// derived from the mcp package's source-of-truth names so a rename there fails
// the build / shifts here instead of silently zeroing the counter.
var tavilySearchExposedName = mcp.ExposedToolName(tavilyServerName, mcp.TavilySearchToolName)

// fetchObscuraFallback retries a failed fetch via obscura's headless browser.
// It only fires for the fetch tool when obscura is configured and the call
// carried a URL. On success it returns the obscura snapshot and true; otherwise
// it returns ok=false so the caller surfaces the original fetch failure.
func (t *Run) fetchObscuraFallback(ctx context.Context, toolName string, arguments map[string]any, round int, reg *webSourceRegistry) (string, bool) {
	if toolName != fetchToolName {
		return "", false
	}
	if !t.e.mcp.HasTool(obscuraNavigateToolName) || !t.e.mcp.HasTool(obscuraSnapshotToolName) {
		return "", false
	}
	url, ok := arguments["url"].(string)
	if !ok || strings.TrimSpace(url) == "" {
		return "", false
	}
	if _, err := t.e.mcp.CallTool(ctx, obscuraNavigateToolName, map[string]any{"url": url}); err != nil {
		slog.Warn("obscura fallback navigate failed", "url", url, "round", round, "err", err)
		return "", false
	}
	snapshot, err := t.e.mcp.CallTool(ctx, obscuraSnapshotToolName, map[string]any{})
	if err != nil {
		slog.Warn("obscura fallback snapshot failed", "url", url, "round", round, "err", err)
		return "", false
	}
	slog.Info("fetch failed, obscura fallback succeeded", "url", url, "round", round, "result_bytes", len(snapshot))
	usage.Record(t.e.usage, "obscura_fetch", func() error { return t.e.usage.IncObscuraFetch(ctx, t.user.ID) })
	// The requested fetch URL is the source; annotate the rendered snapshot with
	// its [n] marker so the model cites it inline like any other web source.
	return capToolOutput(prependURLSource(url, snapshot, reg)), true
}

// availableTools assembles the tool set injected into the prompt for this turn:
// the built-ins the registry offers, then the MCP tools. The always-on core
// (cross-thread memory, directives, web search) is offered unconditionally; the
// heavier optional groups are gated by the turn's toolGate so a simple turn no
// longer ships file-generation schemas or coding-doc MCP tools it will never
// use. gate is a widen-only signal, so gating can only omit tools the turn is
// unlikely to need — never one the model has already been told to use. Called
// once per turn (not per round); the trimmed set is reused across all tool
// rounds.
func (s *Engine) availableTools(thread chat.Thread, gate toolGate) []llm.Tool {
	tools := []llm.Tool(nil)
	names := map[string]string{}
	specs, _ := s.registry()
	for _, spec := range specs {
		if spec.offered != nil && !spec.offered(s, thread, gate) {
			continue
		}
		// A copy of the shared definition; its Parameters map is never written.
		tool := spec.tool
		if owner, exists := names[tool.Function.Name]; exists {
			slog.Warn("skipping duplicate built-in tool name", "tool", tool.Function.Name, "existing", owner)
			continue
		}
		names[tool.Function.Name] = "built_in"
		tools = append(tools, tool)
	}
	if s.mcp != nil {
		// MCP servers are gated by their declared categories: a category-neutral
		// server (no categories, e.g. web search) is always offered, while a
		// category-tagged server (e.g. context7 -> "coding") is offered only when
		// its category is active for this turn. With no category signal at all
		// (widenAll) every server is offered, so a legacy empty-category thread
		// never loses tools it had before gating.
		mcpTools := s.mcp.Tools()
		if !gate.widenAll() {
			mcpTools = s.mcp.ToolsFor(gate.activeCategories())
		}
		for _, tool := range mcpTools {
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
		if tool.Function.Name == imagegen.ToolName {
			selected := tool
			return &selected
		}
	}
	return nil
}

// executeBuiltInTool runs a tool loom implements itself. It returns the
// model-facing output, the artifacts the call created (run_python can write
// several) and whether the name was a built-in at all.
func (t *Run) executeBuiltInTool(ctx context.Context, call llm.ToolCall) (string, []artifact.Response, bool) {
	_, byName := t.e.registry()
	spec := byName[call.Function.Name]
	if spec == nil || spec.run == nil {
		return "", nil, false
	}
	output, created := spec.run(ctx, t, call)
	return output, created, true
}

func oneArtifact(resp *artifact.Response) []artifact.Response {
	if resp == nil {
		return nil
	}
	return []artifact.Response{*resp}
}

// runDocGenerator executes a file-generating built-in tool (create_pdf_file,
// create_docx_file, …) and returns the model-facing output plus any artifact
// response. Unlike the MCP path (finishToolCall), these tools previously logged
// nothing on failure — a "tool failed: …" string went only to the model, leaving
// server-side failures invisible. The deferred log fixes that: every outcome is
// recorded with the tool name, argument size, a truncated argument preview and
// the result, so the next failure is diagnosable from the logs.
func (t *Run) runDocGenerator(ctx context.Context, call llm.ToolCall, generator docgen.Generator) (output string, resp *artifact.Response) {
	start := time.Now()
	defer func() {
		attrs := []any{
			"tool", call.Function.Name,
			"thread_id", t.thread.ID,
			"user_id", t.user.ID,
			"arg_bytes", len(call.Function.Arguments),
			"args", summarizeForLog(call.Function.Arguments),
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if strings.HasPrefix(output, "tool failed") {
			slog.Warn("builtin tool failed", append(attrs, "output", output)...)
		} else {
			slog.Info("builtin tool completed", append(attrs, "result", output)...)
		}
	}()

	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return capToolOutput("tool failed: invalid arguments: " + err.Error()), nil
	}
	filename, _ := args["filename"].(string)
	var buffer bytes.Buffer
	meta, err := generator.Generate(docgen.GenerateRequest{
		Format:   generator.ToolName(),
		Filename: filename,
		Payload:  args,
		Context:  ctx,
	}, &buffer)
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return "tool failed: generated file is too large", nil
	}
	created, err := t.e.persistArtifactBytes(ctx, t.user, t.thread, artifactSpec{
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
		Data:            buffer.Bytes(),
		Thumbnail:       true,
	})
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil
	}
	response := created.Response()
	_ = t.stream.SendJSON("artifact", response)
	return fmt.Sprintf("created artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), &response
}

// maxTypographyImageSide caps typography-model (FLUX.2 [max]) output to the same
// ~1024 px ceiling as the [pro] default; max otherwise supports up to 4 MP.
const maxTypographyImageSide = 1024

// resolveThreadImageModel returns the image model to use for this thread, locking
// the choice on the first image generated in it. If the thread has no locked
// model yet it picks the typography model when typography routing is configured
// and the turn is legible-text/logo work, otherwise the default model, then
// persists the choice via the atomic set-if-empty. Every later image in the thread
// reuses the locked value, so the model never flip-flops mid-conversation. If
// persistence fails it falls back to the in-memory decision so generation still
// proceeds (an empty result defers to the provider's configured default).
//
// Typography is signalled two ways, OR'd: the semantic gate's language-agnostic
// NeedsText flag (authoritative on the required-image path), and a lexical scan of
// the model-authored compiled prompt (which covers a self-initiated generate_image
// in the normal tool loop, where no gate flag flows).
func (s *Engine) resolveThreadImageModel(ctx context.Context, userID string, thread chat.Thread, typography bool, compiledPrompt string) string {
	if locked := strings.TrimSpace(thread.ImageModel); locked != "" {
		return locked
	}
	candidate := s.imageDefaultModel
	if tm := strings.TrimSpace(s.imageTypographyModel); tm != "" && (typography || isTypographyImageRequest(compiledPrompt)) {
		candidate = tm
	}
	if updated, _, err := s.thread.SetThreadImageModelIfEmpty(ctx, userID, thread.ID, candidate); err == nil {
		if locked := strings.TrimSpace(updated.ImageModel); locked != "" {
			return locked
		}
	}
	return candidate
}

// executeImageTool runs an image tool call with the turn's edit source and
// typography routing. It returns the model-facing output and the artifact
// response, nil on failure.
func (t *Run) executeImageTool(ctx context.Context, call llm.ToolCall, generator imagegen.Tool) (string, *artifact.Response) {
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return capToolOutput("tool failed: invalid arguments: " + err.Error()), nil
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
	if aspect, _ := args["aspect_ratio"].(string); aspect != "" {
		req.AspectRatio = aspect
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
	// Forward the user's uploaded/prior image so the model edits the actual pixels
	// instead of a lossy text re-description. Injected here (never from LLM args).
	if editSource := t.plan.editSource; editSource != nil && len(editSource.Data) > 0 {
		req.InputImages = [][]byte{editSource.Data}
		// An edit keeps the source image's proportions unless the turn asked for
		// something else: restyling a 16:9 photo must not hand back a square that
		// crops or squashes the composition. The source's own shape is known here,
		// so nothing has to be guessed from the prompt.
		if req.AspectRatio == "" && req.Width == 0 && req.Height == 0 && editSource.Width > 0 && editSource.Height > 0 {
			req.AspectRatio = imagegen.AspectRatioForSize(editSource.Width, editSource.Height)
		}
	}
	// Pick (and lock, once per thread) the image model. When it is the typography
	// model, clamp output to ≤1024 px/side so flex matches the klein default's size.
	req.Model = t.e.resolveThreadImageModel(ctx, t.user.ID, t.thread, t.plan.imageRoute.typography, req.Prompt)
	if tm := strings.TrimSpace(t.e.imageTypographyModel); tm != "" && req.Model == tm {
		req.Width, req.Height = imagegen.ClampMaxSide(req.Width, req.Height, maxTypographyImageSide)
	}
	var buffer bytes.Buffer
	// A chat turn attributes its stream context up front (httpapi's handleStreamMessage), so
	// this usually finds the attribution already in place and leaves it alone. It
	// stays as a backstop for any dispatch path that reaches here on a context
	// without metadata — otherwise the image model would be the one call in a turn
	// whose log line cannot be tied back to a user or thread.
	meta, err := generator.Generate(inference.WithAttribution(ctx, t.user.ID, t.user.Username, t.thread.ID), req, &buffer)
	if err != nil {
		output := capToolOutput("tool failed: " + err.Error())
		slog.Warn("image tool failed",
			"tool", call.Function.Name,
			"thread_id", t.thread.ID,
			"provider_error", err)
		return output, nil
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return "tool failed: generated image is too large", nil
	}
	created, err := t.e.persistArtifactBytes(ctx, t.user, t.thread, artifactSpec{
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
		MIMEType:        meta.MIMEType,
		Data:            buffer.Bytes(),
		Thumbnail:       true,
	})
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil
	}
	response := created.Response()
	response.Model = meta.Model
	response.Provider = meta.Provider
	response.Width = meta.Width
	response.Height = meta.Height
	response.DurationMs = meta.DurationMs
	usage.Record(t.e.usage, "image_gen", func() error { return t.e.usage.IncImageGen(ctx, t.user.ID) })
	_ = t.stream.SendJSON("artifact", response)
	return fmt.Sprintf("created image artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), &response
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
	return truncateBytesOnRuneBoundary(output, maxToolResultContentBytes)
}

// summarizeForLog trims a value (e.g. tool arguments) to a length that is safe
// to log: enough to debug, short enough not to flood the logs.
func summarizeForLog(value string) string {
	const maxLen = 256
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	return truncateBytesOnRuneBoundary(value, maxLen) + truncationEllipsis
}

func parseToolArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	if args == nil {
		// A literal `null` (or a JSON null the provider emits for absent args)
		// unmarshals to a nil map with no error; return an empty map so callers can
		// safely read and write entries (e.g. injecting include_favicon).
		return map[string]any{}, nil
	}
	return args, nil
}
