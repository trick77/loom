package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RunSummaryMaxChars caps a run summary (runes). The start page card shows it
// whole, about two lines; the bell inbox clamps it to two of its narrower ones.
const RunSummaryMaxChars = 200

// runSummaryMaxCompletionTokens leaves headroom over the ~70 tokens a
// 200-character summary takes, so a slightly verbose reply still finishes.
const runSummaryMaxCompletionTokens = 512

var runSummarySystemPrompt = fmt.Sprintf("You summarize the answer of a recurring research task for a notification. You are given the task and the answer. "+
	"Reply with ONLY the summary: at most two short plain sentences and at most %d characters, leading with the concrete things the answer reports (names, numbers, dates, places). "+
	"No markdown, no citation numbers such as [1], no preamble, no label, no quotes, and do not mention the task, the answer or that this is a summary.", RunSummaryMaxChars)

// SummarizeRun writes the short summary a scheduled run's notification shows.
// It is prose a reader keeps, so it runs on the chat model, not the gate. An
// empty or cut-off reply is an error: the caller falls back to the answer's
// first line. The caller also flattens it to plain text.
func (c *Client) SummarizeRun(ctx context.Context, task, answer, responseLanguage string) (string, error) {
	framed := fmt.Sprintf("Task:\n\"\"\"\n%s\n\"\"\"\n\nAnswer:\n\"\"\"\n%s\n\"\"\"\n\nSummary:",
		strings.TrimSpace(task), strings.TrimSpace(answer))
	messages := []Message{
		{Role: "system", Content: appendLanguageDirective(runSummarySystemPrompt, responseLanguage)},
		{Role: "user", Content: framed},
	}
	reply, err := c.prose(ctx, messages, runSummaryMaxCompletionTokens)
	if err != nil {
		return "", err
	}
	if reply.FinishReason == "length" {
		return "", errors.New("run summary was cut off")
	}
	summary := unwrapSummary(reply.Content)
	if reply.Empty || summary == "" {
		return "", errors.New("run summary reply is empty")
	}
	return summary, nil
}

// summaryQuotes pairs each opening quote a model may wrap a reply in with
// the closing ones it may use.
var summaryQuotes = map[rune]string{'"': `"`, '\'': `'`, '“': `”“`, '„': `“”`, '«': `»`, '»': `«`, '‘': `’`, '‚': `‘’`}

// unwrapSummary takes off an echoed "Summary:" label and quotes wrapping the
// whole reply; a lone trailing apostrophe stays.
func unwrapSummary(reply string) string {
	reply = strings.TrimSpace(reply)
	if label, rest, ok := strings.Cut(reply, ":"); ok && strings.EqualFold(strings.TrimSpace(label), "summary") {
		reply = strings.TrimSpace(rest)
	}
	r := []rune(reply)
	if len(r) >= 2 {
		if closers, ok := summaryQuotes[r[0]]; ok && strings.ContainsRune(closers, r[len(r)-1]) {
			reply = strings.TrimSpace(string(r[1 : len(r)-1]))
		}
	}
	return reply
}
