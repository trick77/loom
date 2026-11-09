package llm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// runSummaryMaxChars caps a run summary (runes): it is a notification body,
// a web push text and a start page card, all of which show two lines.
const runSummaryMaxChars = 280

// runSummaryMaxCompletionTokens leaves headroom over the ~100 tokens a
// 280-character summary takes, so a slightly verbose reply still finishes.
const runSummaryMaxCompletionTokens = 512

const runSummarySystemPrompt = "You summarize the answer of a recurring research task for a notification. You are given the task and the answer. " +
	"Reply with ONLY the summary: at most two plain sentences and at most 280 characters, naming the concrete things the answer reports (names, numbers, dates, places). " +
	"No markdown, no citation numbers such as [1], no preamble, no quotes, and do not mention the task, the answer or that this is a summary."

var runSummaryCitation = regexp.MustCompile(`\s*\[\d+\]`)

// SummarizeRun writes the short summary a scheduled run's notification shows.
// It is prose a reader keeps, so it runs on the chat model, not the gate. An
// empty or cut-off reply is an error: the caller falls back to the answer's
// first line.
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
	summary := cleanRunSummary(reply.Content)
	if reply.Empty || summary == "" {
		return "", errors.New("run summary reply is empty")
	}
	return summary, nil
}

// cleanRunSummary flattens a summary to one plain line within the cap.
func cleanRunSummary(summary string) string {
	summary = runSummaryCitation.ReplaceAllString(summary, "")
	summary = strings.Join(strings.Fields(summary), " ")
	summary = strings.TrimSpace(strings.Trim(summary, `"'`))
	if len([]rune(summary)) <= runSummaryMaxChars {
		return summary
	}
	// One rune is kept for the ellipsis.
	return strings.TrimRight(truncateToMaxChars(summary, runSummaryMaxChars-1), ".") + "…"
}
