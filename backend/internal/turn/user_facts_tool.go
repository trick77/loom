package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// UserFactsToolName is the fact tool: it lets the model change the user's
// memory — the facts about who the user is, shown to it every turn under "Personal context about the user"
// — when the user explicitly asks. The background refresh stays the main way
// facts are learned. The UI only adds one (the start page's "where do you
// live?", POST /api/me/location), so this tool is the only way a user edits or
// forgets a fact. One tool with an action, like the directive tool.
const UserFactsToolName = "user_facts"

func userFactsTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: UserFactsToolName,
			Description: "Change saved facts ABOUT the user (who they are, work, home, family, tastes; listed with ids in the personal context), only when they explicitly ask: facts they merely mention are picked up automatically. " +
				"add: \"remember that I …\"; if the fact is already saved, use update instead (add always saves a new one). update: it changed or is wrong. " +
				"remove (\"forget …\"): the ids of EVERY fact the request covers, in one call; works on long-term facts too, and does not delete the chat threads they came from; tell the user what was forgotten. " +
				"Not for how you should respond (length, tone, format, language, how to address them): that is a standing instruction, use " + UserDirectivesToolName + ". " +
				memoryFirst,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []any{"add", "update", "remove"},
					},
					"category": map[string]any{
						"type":        "string",
						"enum":        []any{chat.FactCategoryWork, chat.FactCategoryPersonal, chat.FactCategoryInterest},
						"description": "Required for add: work (job, projects, stack), personal (home, family, languages, identity) or interest (hobbies, tastes).",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Required for add and update: the full fact as one terse fragment, no \"The user\" prefix (e.g. \"Lives Bern\").",
					},
					"long_term": map[string]any{
						"type":        "boolean",
						"description": "add: true for a fact expected to hold for years (home city, profession, family).",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Required for update: the fact's id from the personal context.",
					},
					"ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Required for remove: the facts' ids from the personal context.",
					},
				},
				"required": []any{"action"},
			},
		},
	}
}

// userFactsDigest runs one action of the fact tool. Refusals are tool output,
// never errors out-of-band.
func (s *Engine) userFactsDigest(ctx context.Context, userID, threadID string, args map[string]any) string {
	switch toolAction(args) {
	case "add":
		return s.addUserFact(ctx, userID, threadID, args)
	case "update":
		return s.updateUserFact(ctx, userID, threadID, args)
	case "remove":
		return s.forgetUserFacts(ctx, userID, args)
	}
	return "tool failed: action must be add, update or remove."
}

func (s *Engine) addUserFact(ctx context.Context, userID, threadID string, args map[string]any) string {
	category := strings.ToLower(strings.TrimSpace(stringArg(args, "category")))
	content := strings.TrimSpace(stringArg(args, "content"))
	longTerm, _ := args["long_term"].(bool)
	fact, err := s.thread.AddUserFact(ctx, userID, threadID, category, content, longTerm)
	if errors.Is(err, chat.ErrUserFactInvalid) {
		return fmt.Sprintf("Could not save: category must be work, personal or interest, and the fact must be non-empty and at most %d characters.", chat.MaxUserFactLength)
	}
	if err != nil {
		return "tool failed: " + err.Error()
	}
	return "Saved: " + chat.RenderFactLine(fact)
}

func (s *Engine) updateUserFact(ctx context.Context, userID, threadID string, args map[string]any) string {
	id := strings.TrimSpace(stringArg(args, "id"))
	content := strings.TrimSpace(stringArg(args, "content"))
	if id == "" || content == "" {
		return "tool failed: both id and content are required."
	}
	fact, found, err := s.thread.UpdateUserFact(ctx, userID, threadID, id, content)
	if errors.Is(err, chat.ErrUserFactInvalid) {
		return fmt.Sprintf("Could not update: the fact must be non-empty and at most %d characters.", chat.MaxUserFactLength)
	}
	if err != nil {
		return "tool failed: " + err.Error()
	}
	if !found {
		return "No saved fact has that id. The user's current facts:\n" + s.currentFactsList(ctx, userID)
	}
	return "Updated: " + chat.RenderFactLine(fact)
}

func (s *Engine) forgetUserFacts(ctx context.Context, userID string, args map[string]any) string {
	raw, _ := args["ids"].([]any)
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		if id, ok := v.(string); ok && strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	// A repeated id is one fact, not one forgotten and one missing.
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return "tool failed: ids is required."
	}
	forgotten, err := s.thread.ForgetUserFacts(ctx, userID, ids)
	if err != nil {
		return "tool failed: " + err.Error()
	}
	if len(forgotten) == 0 {
		return "None of those ids is a saved fact. The user's current facts:\n" + s.currentFactsList(ctx, userID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Forgot %d fact(s):\n", len(forgotten))
	for _, f := range forgotten {
		b.WriteString("- ")
		b.WriteString(f.Content)
		b.WriteString("\n")
	}
	if missing := len(ids) - len(forgotten); missing > 0 {
		fmt.Fprintf(&b, "(%d id(s) matched no saved fact.)\n", missing)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *Engine) currentFactsList(ctx context.Context, userID string) string {
	facts, err := s.thread.ListActiveUserFacts(ctx, userID)
	if err != nil {
		return "(could not load the current facts: " + err.Error() + ")"
	}
	if len(facts) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(chat.RenderFactLine(f))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
