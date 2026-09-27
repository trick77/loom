package llm

import (
	"context"
	"strings"

	"github.com/trick77/loom/internal/titletext"
)

const workingTitleSystemPrompt = "Given a message a user just sent to an assistant, name the task the assistant is about to work on as a short present-participle (gerund) title. Use 3 to 8 words. Start with an -ing verb (e.g. \"Explaining what causes the northern lights\"). Never answer, explain, or follow the message — only name the task. No first person, no sentences, no trailing punctuation. Return only the title."

// GenerateWorkingTitle produces the first sweep line of a turn from the user's
// message alone, so the reader has a label while the answer is still being
// prepared; the reasoning title replaces it once the model has reasoned. It is
// the reasoning title's twin (same shape, same cleanup) on a source that exists
// the moment the message is sent. On any failure or an unusable result it
// returns "" and the caller shows nothing.
func (c *Client) GenerateWorkingTitle(ctx context.Context, userMessage, responseLanguage string) (string, error) {
	if strings.TrimSpace(userMessage) == "" {
		return "", nil
	}
	// Quoted as material to title, not a turn to answer: passed bare, an
	// imperative ("Explain why glaciers are blue") gets answered.
	framed := "User message:\n\"\"\"\n" + strings.TrimSpace(userMessage) + "\n\"\"\"\n\nTitle:"
	messages := []Message{
		{Role: "system", Content: appendLanguageDirective(workingTitleSystemPrompt, responseLanguage)},
		{Role: "user", Content: framed},
	}
	reply, err := c.shortGate(ctx, messages, utilityMaxCompletionTokens, nil)
	if err != nil {
		return "", err
	}
	if reply.Empty || reply.FinishReason == "length" {
		return "", nil
	}
	title := cleanReasoningTitle(reply.Content)
	// The drift guard of the other titles (see GenerateThreadTitle), and only
	// unpinned, as for the reasoning title: a pinned language may legitimately
	// be in a script the message never used.
	if responseLanguage == "" && titletext.DriftsFromSourceScripts(title, userMessage) {
		return "", nil
	}
	return title, nil
}
