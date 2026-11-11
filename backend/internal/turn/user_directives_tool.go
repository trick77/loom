package turn

import (
	"context"
	"errors"
	"strings"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// UserDirectivesToolName is the directive tool: it lets the model manage the
// user's "other instructions" — the explicit, user-steered standing
// instructions shown to it every turn under "Standing instructions the user
// has explicitly asked you to follow". It is the ONLY way that layer is
// mutated (the UI is read-only). Facts about the user are a separate layer
// with their own tool (user_facts_tool.go).
//
// One tool with an action, not one per action: it rides in every turn's
// tools, and the three separate schemas repeated the same rules.
const UserDirectivesToolName = "user_directives"

func userDirectivesTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: UserDirectivesToolName,
			Description: "Change the user's standing instructions (listed with ids in your context): durable rules for how you respond or address them, in every future conversation. " +
				"Only on an explicit request: add on \"always …\", \"from now on …\", \"remember to …\", \"never …\", \"call me …\" (no duplicates); update on \"make that …\"; remove on \"forget that\", \"stop …\". " +
				"Not for facts about the user (home, job, tastes): use " + UserFactsToolName + ". Not for one-off task details. " +
				"Returns the updated list with ids.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []any{"add", "update", "remove"},
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Required for update and remove: the instruction's id from your context.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Required for add and update: the full instruction as a short directive (e.g. \"Always answer in metric units\").",
					},
				},
				"required": []any{"action"},
			},
		},
	}
}

// stringArg reads a string tool-call argument, returning "" when absent or not a
// string.
func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

// toolAction reads a memory tool's action argument, trimmed and lower-cased.
// Both tools call deleting "remove" and take "forget" for it too: the user
// says "forget that" to either layer.
func toolAction(args map[string]any) string {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
	if action == "forget" {
		return "remove"
	}
	return action
}

// userDirectivesDigest runs one action of the directive tool. Each action
// mutates the store then echoes the post-mutation list, so the model acts on
// current state rather than the (now stale) snapshot in its system prompt.
// It never returns errors out-of-band — failures surface as plain tool output.
func (s *Engine) userDirectivesDigest(ctx context.Context, userID string, args map[string]any) string {
	switch toolAction(args) {
	case "add":
		return s.addUserDirective(ctx, userID, args)
	case "update":
		return s.updateUserDirective(ctx, userID, args)
	case "remove":
		return s.removeUserDirective(ctx, userID, args)
	}
	return "tool failed: action must be add, update or remove."
}

func (s *Engine) addUserDirective(ctx context.Context, userID string, args map[string]any) string {
	content := strings.TrimSpace(stringArg(args, "content"))
	if content == "" {
		return "tool failed: content is required."
	}
	if _, err := s.thread.AddUserDirective(ctx, userID, content); err != nil {
		if errors.Is(err, chat.ErrDirectivesBudgetExceeded) {
			return "Could not save: the user's standing-instructions budget is full, or this instruction is too long. Ask them which existing instruction to remove or shorten first.\n\n" + s.currentDirectivesList(ctx, userID)
		}
		return "tool failed: " + err.Error()
	}
	return "Saved. The user's current standing instructions:\n" + s.currentDirectivesList(ctx, userID)
}

func (s *Engine) removeUserDirective(ctx context.Context, userID string, args map[string]any) string {
	id := strings.TrimSpace(stringArg(args, "id"))
	if id == "" {
		return "tool failed: id is required."
	}
	found, err := s.thread.RemoveUserDirective(ctx, userID, id)
	if err != nil {
		return "tool failed: " + err.Error()
	}
	if !found {
		return "No saved instruction has that id. The user's current standing instructions:\n" + s.currentDirectivesList(ctx, userID)
	}
	return "Removed. The user's current standing instructions:\n" + s.currentDirectivesList(ctx, userID)
}

func (s *Engine) updateUserDirective(ctx context.Context, userID string, args map[string]any) string {
	id := strings.TrimSpace(stringArg(args, "id"))
	content := strings.TrimSpace(stringArg(args, "content"))
	if id == "" || content == "" {
		return "tool failed: both id and content are required."
	}
	_, found, err := s.thread.ReplaceUserDirective(ctx, userID, id, content)
	if err != nil {
		if errors.Is(err, chat.ErrDirectivesBudgetExceeded) {
			return "Could not update: the new text would exceed the user's standing-instructions budget. Ask them to shorten it or remove another instruction first.\n\n" + s.currentDirectivesList(ctx, userID)
		}
		return "tool failed: " + err.Error()
	}
	if !found {
		return "No saved instruction has that id. The user's current standing instructions:\n" + s.currentDirectivesList(ctx, userID)
	}
	return "Updated. The user's current standing instructions:\n" + s.currentDirectivesList(ctx, userID)
}

// currentDirectivesList loads and renders the user's directives for echoing back
// in tool output. On a load error it says so rather than failing the tool.
func (s *Engine) currentDirectivesList(ctx context.Context, userID string) string {
	directives, err := s.thread.ListUserDirectives(ctx, userID)
	if err != nil {
		return "(could not load the current instructions: " + err.Error() + ")"
	}
	return renderDirectivesList(directives)
}

// renderDirectivesList renders directives as "- [id] content" lines (or "(none)")
// for tool output.
func renderDirectivesList(directives []chat.UserDirective) string {
	if len(directives) == 0 {
		return "(none)"
	}
	return chat.RenderDirectiveLines(directives)
}
