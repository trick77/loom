package llm

import (
	"strconv"
	"strings"

	"github.com/trick77/toolkit/llmwire"
)

// switchNote tells the model a turn routes to which earlier answers in the
// thread another model wrote (a model switch, an image routed to the vision
// model, a changed deployment), by their place among the answers. Replayed as
// plain assistant turns they read as its own words: asked to compare them with
// "the other model", it denied there was one. It names no model. Rows of one
// answer stored apart (a turn split for a queued message) count once, as the
// Messages protocol merges adjacent assistant turns. An answer with no
// recorded model counts as the routed model's own.
func switchNote(messages []Message, model string) string {
	var others []int
	answer, inAnswer, marked := 0, false, false
	for _, m := range messages {
		if m.Role == "user" {
			inAnswer = false
			continue
		}
		if m.Role != "assistant" || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if !inAnswer {
			answer++
			inAnswer, marked = true, false
		}
		if !marked && m.Model != "" && m.Model != model {
			others = append(others, answer)
			marked = true
		}
	}
	if len(others) == 0 {
		return ""
	}
	which := "answer " + positions(others)
	if len(others) > 1 {
		which = "answers " + positions(others)
	}
	return "\n\n[Earlier assistant " + which + " in this thread (counted from the first answer) were written by " +
		"a different model, not by you. If the user asks about them, treat them as that model's work and never " +
		"claim them as your own; otherwise leave this unmentioned.]"
}

// positions spells ascending answer numbers with runs folded into ranges
// ("1-40, 43 and 45"), so a long thread does not grow the note per answer.
func positions(ns []int) string {
	var parts []string
	for i := 0; i < len(ns); {
		j := i
		for j+1 < len(ns) && ns[j+1] == ns[j]+1 {
			j++
		}
		part := strconv.Itoa(ns[i])
		if j > i {
			part += "-" + strconv.Itoa(ns[j])
		}
		parts = append(parts, part)
		i = j + 1
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// withSwitchNote appends switchNote to the request's last user message, like
// the earlier-sources note: the system prompt and the replayed history stay
// the cached prefix, and the note is never stored, so a later turn rebuilds
// it fresh.
func withSwitchNote(wire []llmwire.Message, messages []Message, model string) []llmwire.Message {
	note := switchNote(messages, model)
	if note == "" {
		return wire
	}
	for i := len(wire) - 1; i >= 0; i-- {
		if wire[i].Role != llmwire.RoleUser {
			continue
		}
		if len(wire[i].Parts) > 0 {
			wire[i].Parts = append(wire[i].Parts, llmwire.Part{Kind: llmwire.PartText, Text: note})
		} else {
			wire[i].Text += note
		}
		break
	}
	return wire
}
