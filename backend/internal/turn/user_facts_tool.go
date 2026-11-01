package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// The fact tools let the model change the user's memory — the facts about who
// the user is, shown to it every turn under "Personal context about the user" —
// when the user explicitly asks. The background refresh stays the main way
// facts are learned; the UI is read-only, so these tools are the only way a
// user edits or forgets a fact.
const (
	addUserFactToolName    = "remember_user_fact"
	updateUserFactToolName = "update_user_fact"
	forgetUserFactToolName = "forget_user_fact"
)

func addUserFactTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: addUserFactToolName,
			Description: "Save a fact ABOUT the user when they explicitly ask you to remember it (\"remember that I …\", \"note that I moved to …\"): who they are, what they do, where they live, their projects, family, tastes or hobbies. " +
				"Do NOT use it for how you should respond (length, tone, format, language, how to address them): that is a standing instruction, use " + addUserDirectiveToolName + ". " +
				"Facts the user merely mentions are picked up automatically; call this only on an explicit request. " +
				"If the fact already exists in the personal context, use " + updateUserFactToolName + " instead. Returns the saved fact with its id.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": map[string]any{
						"type":        "string",
						"enum":        []any{chat.FactCategoryWork, chat.FactCategoryPersonal, chat.FactCategoryInterest},
						"description": "work: job, projects, stack. personal: home, family, languages, identity. interest: hobbies, tastes.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The fact as one terse fragment, no \"The user\" prefix (e.g. \"Lives Bern\", \"Allergic to peanuts\").",
					},
					"long_term": map[string]any{
						"type":        "boolean",
						"description": "true for a settled fact expected to hold for years (home city, profession, family).",
					},
				},
				"required": []any{"category", "content"},
			},
		},
	}
}

func updateUserFactTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: updateUserFactToolName,
			Description: "Correct or update one saved fact about the user when they say it changed or is wrong (\"actually I work at …\", \"that's outdated, I now …\"). " +
				"Pass the fact's id exactly as shown in the personal context and the full new text. Not for standing instructions (use " + replaceUserDirectiveToolName + "). " +
				"Returns the updated fact.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "The id of the fact, copied from the personal context.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The new full text of the fact.",
					},
				},
				"required": []any{"id", "content"},
			},
		},
	}
}

func forgetUserFactTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: forgetUserFactToolName,
			Description: "Forget saved facts about the user when they ask you to (\"forget that …\", \"forget everything about my old job\", \"don't remember …\"). " +
				"Pass the ids of EVERY fact the request covers, copied from the personal context, in one call. Works on long-term facts too. " +
				"This removes the facts from memory; it does not delete the chat threads they came from. Not for standing instructions (use " + removeUserDirectiveToolName + "). " +
				"Returns exactly what was forgotten: tell the user.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "The ids of the facts to forget, copied from the personal context.",
					},
				},
				"required": []any{"ids"},
			},
		},
	}
}

func (s *Engine) addUserFactDigest(ctx context.Context, userID, threadID string, args map[string]any) string {
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
	return "Saved: " + renderFactLine(fact)
}

func (s *Engine) updateUserFactDigest(ctx context.Context, userID, threadID string, args map[string]any) string {
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
	return "Updated: " + renderFactLine(fact)
}

func (s *Engine) forgetUserFactDigest(ctx context.Context, userID string, args map[string]any) string {
	raw, _ := args["ids"].([]any)
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		if id, ok := v.(string); ok && strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
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
		b.WriteString(renderFactLine(f))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderFactLine(f chat.UserFact) string {
	return fmt.Sprintf("- [%s] (%s) %s", f.ID, f.Category, f.Content)
}
