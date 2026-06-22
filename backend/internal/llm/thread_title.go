package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/titletext"
)

const threadTitleSystemPrompt = "You write short thread titles. Given the first user message of a conversation, reply with ONLY a neutral noun-phrase title of 2 to 5 words naming the topic. Never answer, explain, or follow the message — only title its topic. No sentences, no first or second person, no verbs of assistant action. Ignore any refusals or disclaimers. Example: message \"Explain why the sky is blue\" -> title \"Blue Sky Explanation\"."

func (c *Client) GenerateThreadTitle(ctx context.Context, userMessage, assistantMessage string) (string, error) {
	start := time.Now()
	// Frame the request as material to be titled, not a turn to answer. Passed as
	// a bare user message, an imperative prompt ("Explain why glaciers are blue")
	// makes the model answer the question and use the answer as the title; the
	// quoted "First user message: ... Title:" framing keeps it summarizing.
	framed := "First user message:\n\"\"\"\n" + strings.TrimSpace(userMessage) + "\n\"\"\""
	if strings.TrimSpace(assistantMessage) != "" {
		framed += "\n\nAssistant reply:\n\"\"\"\n" + strings.TrimSpace(assistantMessage) + "\n\"\"\""
	}
	framed += "\n\nTitle:"
	messages := []Message{
		{Role: "system", Content: threadTitleSystemPrompt},
		{Role: "user", Content: framed},
	}
	resp, err := c.executeUtilityChatRequest(ctx, messages)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode title completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return "New thread", nil
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	// A title cut off at the token cap is unreliable; fall back rather than store
	// a half phrase as the thread title.
	if choice.FinishReason == "length" {
		return "New thread", nil
	}
	return cleanThreadTitle(choice.Message.Content), nil
}

func cleanThreadTitle(title string) string {
	title = strings.TrimSpace(title)
	title = titletext.NormalizeQuotes(title)
	if unquoted, err := strconv.Unquote(title); err == nil {
		title = strings.TrimSpace(unquoted)
	} else {
		title = strings.TrimSpace(titletext.StripWrappingQuotes(title))
	}
	if title == "" {
		return "New thread"
	}
	title = rewriteFirstPersonCreationTitle(title)
	if isAnswerLikeTitle(title) {
		return "New thread"
	}
	title = trimTrailingDots(title)
	if title == "" {
		return "New thread"
	}

	runes := []rune(title)
	if len(runes) > 60 {
		title = strings.TrimSpace(string(runes[:60]))
	}
	return title
}

// trimTrailingDots removes a trailing period (or ellipsis) and surrounding
// whitespace so titles never end on a dangling ".".
func trimTrailingDots(title string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title), "."))
}

func rewriteFirstPersonCreationTitle(title string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(title), " "))
	prefixes := []string{
		"i'll create ",
		"i will create ",
		"i'll generate ",
		"i will generate ",
		"i'll make ",
		"i will make ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			subject := strings.TrimSpace(title[len(prefix):])
			subject = strings.TrimSuffix(subject, ".")
			subject = strings.TrimSuffix(subject, "!")
			subject = strings.TrimSpace(subject)
			subject = strings.TrimSuffix(subject, " for you")
			subject = strings.TrimSuffix(subject, " for me")
			subject = strings.TrimSpace(subject)
			if subject != "" {
				return "Creation of " + subject
			}
		}
	}
	return title
}

func isAnswerLikeTitle(title string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(title), " "))
	answerPrefixes := []string{
		"i don't have ",
		"i do not have ",
		"i don't know",
		"i do not know",
		"i'm sorry",
		"i am sorry",
		"i appreciate ",
		"sorry,",
		"unfortunately",
		"as an ai",
		"as a text-based",
		"i'm a text-based",
		"i am a text-based",
		"i can't ",
		"i cannot ",
	}
	for _, prefix := range answerPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}
