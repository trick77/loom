package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// The directive tools let the model manage the user's "other instructions" — the
// explicit, user-steered standing instructions shown to it every turn under
// "Standing instructions the user has explicitly asked you to follow". They are
// the ONLY way that layer is mutated (the UI is read-only); derived memory is
// never touched by a tool.
const (
	addUserDirectiveToolName     = "remember_user_directive"
	removeUserDirectiveToolName  = "forget_user_directive"
	replaceUserDirectiveToolName = "update_user_directive"
)

func addUserDirectiveTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: addUserDirectiveToolName,
			Description: "Save a NEW standing instruction the user has explicitly asked you to follow in every future conversation — a durable preference, rule, or way they want you to behave or be addressed. " +
				"Call this ONLY on an explicit request like \"always …\", \"from now on …\", \"remember to …\", \"never …\", \"call me …\". " +
				"Do NOT use it for passing facts about the user, one-off task details, or things they merely mention — only deliberate, lasting instructions. " +
				"The current standing instructions (with their ids) are listed in your context; do not add one that duplicates an existing instruction. " +
				"Returns the updated list of standing instructions with their ids.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The instruction to save, phrased as a short standing directive (e.g. \"Always answer in metric units\", \"Call me Jan\").",
					},
				},
				"required": []any{"content"},
			},
		},
	}
}

func removeUserDirectiveTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: removeUserDirectiveToolName,
			Description: "Delete one of the user's saved standing instructions when they ask you to forget it or stop doing it (\"forget that\", \"you don't need to … anymore\", \"stop …\"). " +
				"Pass the id of the instruction exactly as shown in the standing-instructions list in your context. " +
				"Returns the updated list of standing instructions.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "The id of the saved instruction to delete, copied from the standing-instructions list in your context.",
					},
				},
				"required": []any{"id"},
			},
		},
	}
}

func replaceUserDirectiveTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: replaceUserDirectiveToolName,
			Description: "Update the wording of one of the user's saved standing instructions when they ask to change it (\"actually, make that …\", \"change my … to …\"). " +
				"Pass the id from the standing-instructions list in your context and the full new text. " +
				"Returns the updated list of standing instructions.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "The id of the saved instruction to replace, copied from the standing-instructions list in your context.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The new full text for this instruction.",
					},
				},
				"required": []any{"id", "content"},
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

// addUserDirectiveDigest, removeUserDirectiveDigest and replaceUserDirectiveDigest
// mutate the store then echo the post-mutation list, so the model acts on current
// state rather than the (now stale) snapshot in its system prompt. They never
// return errors out-of-band — failures surface as plain tool output.

func (s *server) addUserDirectiveDigest(ctx context.Context, userID string, args map[string]any) string {
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

func (s *server) removeUserDirectiveDigest(ctx context.Context, userID string, args map[string]any) string {
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

func (s *server) replaceUserDirectiveDigest(ctx context.Context, userID string, args map[string]any) string {
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
func (s *server) currentDirectivesList(ctx context.Context, userID string) string {
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
	var b strings.Builder
	for _, d := range directives {
		b.WriteString("- [")
		b.WriteString(d.ID)
		b.WriteString("] ")
		b.WriteString(strings.TrimSpace(d.Content))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// handleGetUserDirectives backs the read-only Memories-page directives view. There
// is no create/update/delete endpoint — those go through the chat tools above.
func (s *server) handleGetUserDirectives(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	directives, err := s.thread.ListUserDirectives(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "list user directives failed")
		return
	}
	if directives == nil {
		directives = []chat.UserDirective{}
	}
	writeJSON(w, directives)
}
