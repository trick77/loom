package turn

import (
	"context"
	"encoding/base64"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/inference"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// imageToolName is the image tool as loom addresses it: server imagegen, tool
// generate (the contract in docs/imagegen.md). Whatever serves that name, the
// built-in or an mcp.json server, is the one an image turn forces.
var imageToolName = mcp.ExposedToolName(mcp.ImagegenServer, mcp.ImagegenTool)

// The arguments loom fills for the image tool (docs/imagegen.md). The server
// declares them; loom strips them from the tool it offers the model and adds
// them at dispatch.
const (
	argInputImages = mcp.ImagegenArgInputImages
	argTextHeavy   = mcp.ImagegenArgTextHeavy
	argModel       = mcp.ImagegenArgModel
)

var imageLoomArgs = []string{argInputImages, argTextHeavy, argModel}

// maxImagePromptRunes caps the user's own text when loom writes the image
// call itself (fallbackImageToolCall).
const maxImagePromptRunes = mcp.ImagegenMaxPromptRunes

// imageToolAvailable reports whether userID may use an image tool whose
// images can be kept: one configured, open to them, allowed to make
// artifacts, and somewhere to store them. It is the precondition for routing
// a turn to images.
func (s *Engine) imageToolAvailable(userID string) bool {
	if s.mcp == nil || !s.canStoreArtifacts() {
		return false
	}
	return s.mcp.HasTool(imageToolName) && s.mcp.MakesArtifacts(imageToolName) &&
		s.mcp.ToolAllowed(MCPPrincipal(auth.User{ID: userID}), imageToolName)
}

// dropLoomArgs removes the arguments only loom fills from args, in place,
// whatever their case: a JSON decoder matches "Model" to model.
func dropLoomArgs(args map[string]any) {
	for key := range args {
		for _, name := range imageLoomArgs {
			if strings.EqualFold(key, name) {
				delete(args, key)
			}
		}
	}
}

// toolCallContext is the context a round's MCP calls run on: it names the
// caller (a per-user server refuses a call without one) and attributes the
// call's log lines to the user and thread, a backstop for a path that reaches
// here without the stream handler's attribution (an in-process server's model
// call would otherwise log unattributed).
func (t *Run) toolCallContext(ctx context.Context) context.Context {
	ctx = inference.WithAttribution(ctx, t.user.ID, t.user.Username, t.thread.ID)
	return mcp.WithPrincipal(ctx, MCPPrincipal(t.user))
}

// withoutLoomArgs is the image tool as the model sees it: the arguments loom
// fills are gone from its schema. The shared definition is not written.
func withoutLoomArgs(tool llm.Tool) llm.Tool {
	props, ok := tool.Function.Parameters["properties"].(map[string]any)
	if !ok {
		return tool
	}
	trimmed := maps.Clone(props)
	dropLoomArgs(trimmed)
	params := maps.Clone(tool.Function.Parameters)
	params["properties"] = trimmed
	switch required := params["required"].(type) {
	case []any: // decoded from a server's JSON
		params["required"] = slices.DeleteFunc(slices.Clone(required), func(v any) bool {
			name, _ := v.(string)
			return slices.Contains(imageLoomArgs, name)
		})
	case []string:
		params["required"] = slices.DeleteFunc(slices.Clone(required), func(name string) bool {
			return slices.Contains(imageLoomArgs, name)
		})
	}
	tool.Function.Parameters = params
	return tool
}

// loomArgs are the arguments loom adds to a call of the image tool: the
// image to edit, whether the picture must carry legible text, and the model
// the thread's first image used. nil for any other tool.
func (t *Run) loomArgs(call llm.ToolCall) map[string]any {
	if !t.e.toolPolicy(call.Function.Name).imageGenerator {
		return nil
	}
	args, _ := parseToolArguments(call.Function.Arguments)
	prompt, _ := args["prompt"].(string)
	// Two signals, OR'd: the gate's language-agnostic NeedsText on an image
	// turn, and a scan of the model's own prompt when it called the tool
	// unprompted (no gate flag then).
	extra := map[string]any{argTextHeavy: t.plan.imageRoute.typography || isTypographyImageRequest(prompt)}
	if src := t.plan.editSource; src != nil && len(src.Data) > 0 {
		extra[argInputImages] = []map[string]any{{
			"data":      base64.StdEncoding.EncodeToString(src.Data),
			"mime_type": http.DetectContentType(src.Data),
		}}
	}
	if locked := strings.TrimSpace(t.thread.ImageModel); locked != "" {
		extra[argModel] = locked
	}
	return extra
}

// lockImageModel keeps the model of the thread's first image, as the server
// reported it, so every later image is sent with it and the look never flips
// mid-conversation. loom never interprets the name.
func (t *Run) lockImageModel(ctx context.Context, created []artifact.Response) {
	if len(created) == 0 || strings.TrimSpace(t.thread.ImageModel) != "" {
		return
	}
	model := strings.TrimSpace(created[0].Model)
	if model == "" {
		return
	}
	// Like the image itself (keepToolArtifacts), the lock outlives a round cut
	// short or a client gone: an image kept without its model lets the next
	// one change the look.
	lockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), artifactPersistTimeout)
	defer cancel()
	updated, _, err := t.e.thread.SetThreadImageModelIfEmpty(lockCtx, t.user.ID, t.thread.ID, model)
	if err != nil {
		slog.Warn("image model not locked", "thread_id", t.thread.ID, "err", err)
		return
	}
	t.thread.ImageModel = updated.ImageModel
}
