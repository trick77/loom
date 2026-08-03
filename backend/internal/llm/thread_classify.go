package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/classifier"
)

// threadClassifyInstructions asks the helper model to pick exactly one category
// for the conversation. It is deliberately separate from the title call so the
// title prompt stays untouched and this prompt can be tuned purely for routing
// accuracy. The reply is a single category value (a few tokens), not JSON. The
// category list is appended per call (see threadClassifySystemPrompt) because the
// offered set depends on the message.
const threadClassifyInstructions = "You classify the first user message of a conversation into exactly one category. Pick the most specific category that fits, but only a category whose defining input is actually present in the message — url_lookup requires a pasted URL; summarization, translation, and writing_editing require the text itself (or an attachment reference). Use \"knowledge_discovery\" only for informational or educational queries that fit no more specialized category, and \"general\" only for chit-chat or queries that fit nothing else. Reply with ONLY the category value (lowercase, no punctuation, nothing else). Categories:\n"

// threadClassifySystemPrompt builds the classify system prompt for one message.
// When the message contains no URL, url_lookup is withheld from the menu entirely
// — deictic prompts ("which thread here says this") otherwise read to a small
// model as "answer about a specific referenced thing" and leak into url_lookup;
// withholding it makes the model re-pick the true best category itself.
func threadClassifySystemPrompt(userMessage string) string {
	if messageContainsURL(userMessage) {
		return threadClassifyInstructions + classifier.PromptGuide()
	}
	return threadClassifyInstructions + classifier.PromptGuide(classifier.URLLookup)
}

// messageContainsURL reports whether the message contains an explicit link:
// an http/https scheme anywhere, or a "www." at a word boundary (nothing
// alphanumeric before it, so "awww." does not match, and something alphanumeric
// after it, so a bare "www." mentioned as a prefix does not). The boundary scan
// — rather than token splitting — also catches links wrapped in Unicode quotes
// («www.blick.ch») or markdown syntax ([Blick](www.blick.ch)). Deliberately no
// bare-domain matching — ".js" and ".io" are real TLDs, so tokens like "node.js"
// would false-positive and re-open the exact misclassification this guards.
func messageContainsURL(message string) bool {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return true
	}
	for i := 0; ; {
		j := strings.Index(lower[i:], "www.")
		if j < 0 {
			return false
		}
		j += i
		// ASCII byte checks suffice: the string is lowercased, and any byte of a
		// multibyte rune (e.g. «) is >= 0x80, which reads as a boundary here.
		boundaryBefore := j == 0 || !isASCIIAlnum(lower[j-1])
		domainAfter := j+4 < len(lower) && isASCIIAlnum(lower[j+4])
		if boundaryBefore && domainAfter {
			return true
		}
		i = j + 4
	}
}

func isASCIIAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// ClassifyThread picks the prompt-classifier category for a conversation from its
// first user message. It always returns a valid category — General on any request,
// decode, or empty-reply failure — so callers can use the result unconditionally;
// the returned error is informational (for logging) only.
func (c *Client) ClassifyThread(ctx context.Context, userMessage string) (string, error) {
	start := time.Now()
	framed := "First user message:\n\"\"\"\n" + strings.TrimSpace(userMessage) + "\n\"\"\"\n\nCategory:"
	messages := []Message{
		{Role: "system", Content: threadClassifySystemPrompt(userMessage)},
		{Role: "user", Content: framed},
	}
	resp, err := c.executeNonReasoningChatRequest(ctx, messages, utilityMaxCompletionTokens)
	if err != nil {
		logInferenceFailed(ctx, c.nonReasoningModel, time.Since(start), err)
		return string(classifier.General), err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode classify completion response: %w", err)
		logInferenceFailed(ctx, c.nonReasoningModel, time.Since(start), err)
		return string(classifier.General), err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.nonReasoningModel, time.Since(start), completion.Usage, "")
		return string(classifier.General), nil
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.nonReasoningModel, time.Since(start), completion.Usage, choice.FinishReason)
	// Match tolerantly extracts the category from the reply (handling quotes,
	// punctuation, or stray prose) and coerces anything unrecognized — including a
	// truncated "length" reply — to General, so a bad reply never produces a bad
	// category.
	category := classifier.Match(choice.Message.Content)
	// Belt and braces: url_lookup was withheld from the menu when the message has
	// no URL, but the model may still name it. Its block ("rely solely on the page
	// at the URL") is actively harmful without a URL, so coerce to General.
	if category == classifier.URLLookup && !messageContainsURL(userMessage) {
		category = classifier.General
	}
	return string(category), nil
}
