package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/trick77/llmwire"
)

// scheduleParseMaxCompletionTokens fits the one-line JSON reply with room.
const scheduleParseMaxCompletionTokens = 64

const scheduleParseSystemPrompt = `You turn a description of when a recurring task should run into a cron template. Reply with ONLY a JSON object: {"template":"<5 fields>","ok":true}, or {"ok":false} when the text names no recurring time (a condition like "when it rains", or nonsense).

The 5 fields are: minute hour day-of-month month day-of-week (0 = Sunday ... 6 = Saturday). Numbers only, no names.
In each field use:
- "~" when the description leaves the field free: loom draws a random value later.
- "a~b" when it narrows the field to a range: loom draws one value in it.
- a number, a list "1,3" or a range "1-5" when the description fixes it.
- "*" for every value.
Never pick a value yourself where the description leaves the choice open; use "~" or "a~b".
When the hour is free, use "7~22" so the task runs during the day.
"morning" = 6~10, "lunch" = 11~13, "afternoon" = 13~17, "evening" = 18~21, "night" = 22~23.

Examples:
"once a week" -> {"template":"~ 7~22 * * ~","ok":true}
"every day" -> {"template":"~ 7~22 * * *","ok":true}
"every weekday morning" -> {"template":"~ 6~10 * * 1-5","ok":true}
"Mondays after lunch" -> {"template":"~ 13~14 * * 1","ok":true}
"twice a day" -> {"template":"~ 7~10,19~22 * * *","ok":true}
"twice a month" -> {"template":"~ 7~22 1~14,15~28 * *","ok":true}
"every Friday at 18:00" -> {"template":"0 18 * * 5","ok":true}
"on the first of every month" -> {"template":"~ 7~22 1 * *","ok":true}
"every 5 minutes" -> {"template":"*/5 * * * *","ok":true}
"when it rains" -> {"ok":false}

The description may be in any language.`

// ParseSchedule reads a description of a recurring time ("once a week") as a
// cron template whose free fields are "~" for loom to draw. ok is false when
// the text names no recurring time or the reply is unusable.
func (c *Client) ParseSchedule(ctx context.Context, text string) (string, bool, error) {
	messages := []Message{
		{Role: "system", Content: scheduleParseSystemPrompt},
		{Role: "user", Content: "Description:\n\"\"\"\n" + strings.TrimSpace(text) + "\n\"\"\"\n\nJSON:"},
	}
	reply, err := c.shortGate(ctx, messages, scheduleParseMaxCompletionTokens, func(reply completion) []slog.Attr {
		template, ok := parseScheduleReply(reply.Content)
		return []slog.Attr{slog.String("template", template), slog.Bool("ok", ok)}
	})
	if err != nil {
		return "", false, err
	}
	if reply.Empty {
		return "", false, nil
	}
	template, ok := parseScheduleReply(reply.Content)
	return template, ok, nil
}

// parseScheduleReply extracts the template from the gate's JSON reply,
// tolerating a code fence or prose around it.
func parseScheduleReply(reply string) (string, bool) {
	raw, found := llmwire.JSONObject(reply)
	if !found {
		return "", false
	}
	var decoded struct {
		Template string `json:"template"`
		OK       bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", false
	}
	template := strings.Join(strings.Fields(decoded.Template), " ")
	if !decoded.OK || template == "" {
		return "", false
	}
	return template, true
}

// Finding is one item a scheduled run's answer reported as new.
type Finding struct {
	Key   string
	Label string
	// Source is the [n] number the answer cited for the item; 0 when none.
	Source int
}

const findingsMaxCompletionTokens = 1200

// maxKnownKeys bounds the already-reported keys shown to the extractor.
const maxKnownKeys = 200

const findingsSystemPrompt = `You read the answer of a recurring research task and list the items it reports as NEW findings. Reply with ONLY a JSON object: {"items":[{"key":"...","label":"...","source":<n>}]}.

- An item is one distinct thing the answer presents as new: a film, a paper, a decision, a product, an event.
- Leave out items the answer says were already known, reported earlier or are still the same.
- key: a short lowercase ASCII slug naming the item itself, the same in every language and on every run, e.g. "dune-part-three" or "avengers-doomsday". When an item matches one of the known keys, reuse that key exactly.
- label: the item's name as a reader would recognise it, at most 80 characters.
- source: the bracketed source number the answer cites for the item, or 0.
- When the answer reports nothing new, reply {"items":[]}.`

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// ExtractFindings lists the items a scheduled run's answer reports as new,
// reusing knownKeys for items reported before. It never fails a run: a bad
// reply yields no items and the error is for the log.
func (c *Client) ExtractFindings(ctx context.Context, task, answer string, knownKeys []string) ([]Finding, error) {
	if len(knownKeys) > maxKnownKeys {
		knownKeys = knownKeys[:maxKnownKeys]
	}
	known := "(none)"
	if len(knownKeys) > 0 {
		known = strings.Join(knownKeys, ", ")
	}
	framed := fmt.Sprintf("Task:\n\"\"\"\n%s\n\"\"\"\n\nKnown keys: %s\n\nAnswer:\n\"\"\"\n%s\n\"\"\"\n\nJSON:",
		strings.TrimSpace(task), known, strings.TrimSpace(answer))
	messages := []Message{
		{Role: "system", Content: findingsSystemPrompt},
		{Role: "user", Content: framed},
	}
	reply, err := c.shortGate(ctx, messages, findingsMaxCompletionTokens, func(reply completion) []slog.Attr {
		return []slog.Attr{slog.Int("items", len(parseFindings(reply.Content)))}
	})
	if err != nil {
		return nil, err
	}
	if reply.Empty {
		return nil, nil
	}
	return parseFindings(reply.Content), nil
}

func parseFindings(reply string) []Finding {
	raw, found := llmwire.JSONObject(reply)
	if !found {
		return nil
	}
	var decoded struct {
		Items []struct {
			Key    string `json:"key"`
			Label  string `json:"label"`
			Source int    `json:"source"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Finding
	for _, item := range decoded.Items {
		key := strings.Trim(slugInvalid.ReplaceAllString(strings.ToLower(item.Key), "-"), "-")
		label := strings.Join(strings.Fields(item.Label), " ")
		if len(key) > 80 {
			key = strings.Trim(key[:80], "-")
		}
		if key == "" || seen[key] {
			continue
		}
		if label == "" {
			label = key
		}
		if r := []rune(label); len(r) > 80 {
			label = string(r[:80])
		}
		seen[key] = true
		out = append(out, Finding{Key: key, Label: label, Source: max(item.Source, 0)})
	}
	return out
}
