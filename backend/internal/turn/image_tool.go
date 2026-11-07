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
	"github.com/trick77/loom/internal/llm"
)

// imageToolName is the image tool as loom addresses it: server imagegen, tool
// generate (the contract in docs/imagegen.md). Whatever serves that name, the
// built-in or an mcp.json server, is the one an image turn forces.
const imageToolName = "imagegen__generate"

// The arguments loom fills for the image tool (docs/imagegen.md). The server
// declares them; loom strips them from the tool it offers the model and adds
// them at dispatch.
const (
	argInputImages = "input_images"
	argTextHeavy   = "text_heavy"
	argModel       = "model"
)

var imageLoomArgs = []string{argInputImages, argTextHeavy, argModel}

// maxImagePromptRunes caps the user's own text when loom writes the image
// call itself (fallbackImageToolCall); the contract allows 4000 characters.
const maxImagePromptRunes = 4000

// imageToolAvailable reports whether an image tool is configured and its
// images have somewhere to go: the precondition for routing a turn to it.
func (s *Engine) imageToolAvailable() bool {
	if s.mcp == nil || !s.canStoreArtifacts() {
		return false
	}
	_, byName := s.registry()
	for name, spec := range byName {
		if spec.imageGenerator && s.mcp.HasTool(name) {
			return true
		}
	}
	return false
}

// withoutLoomArgs is the image tool as the model sees it: the arguments loom
// fills are gone from its schema. The shared definition is not written.
func withoutLoomArgs(tool llm.Tool) llm.Tool {
	props, ok := tool.Function.Parameters["properties"].(map[string]any)
	if !ok {
		return tool
	}
	trimmed := maps.Clone(props)
	for _, name := range imageLoomArgs {
		delete(trimmed, name)
	}
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
	updated, _, err := t.e.thread.SetThreadImageModelIfEmpty(ctx, t.user.ID, t.thread.ID, model)
	if err != nil {
		slog.Warn("image model not locked", "thread_id", t.thread.ID, "err", err)
		return
	}
	t.thread.ImageModel = updated.ImageModel
}
