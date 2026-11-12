package chat

import (
	"encoding/json"
	"strings"
)

// StripPastedBlocks removes a message's collapsed paste blocks from its
// content, the inverse of the client's fold (the typed text, then each
// block, joined by "\n\n"): blocks are peeled from the end, an exact match
// first, then trimmed (the store trims the whole content). matched reports
// per block whether it was found; an unmatched block's text is still in text.
// The UI's stripPastedBlocks follows the same rules.
func StripPastedBlocks(content string, blocks []MessagePastedText) (string, []bool) {
	matched := make([]bool, len(blocks))
	end := len(content)
	for i := len(blocks) - 1; i >= 0; i-- {
		hay := content[:end]
		text := blocks[i].Text
		at := -1
		if text != "" {
			at = strings.LastIndex(hay, text)
		}
		if at < 0 {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				at = strings.LastIndex(hay, trimmed)
			}
		}
		if at < 0 {
			continue
		}
		matched[i] = true
		end = at
		if strings.HasSuffix(content[:end], "\n\n") {
			end -= 2
		}
	}
	return content[:end], matched
}

// OmittedQuote stands in, in a later turn's history, for the outside text
// a user message carried along (an idea's passage).
const OmittedQuote = "[A quoted passage from outside the chat was here; it is left out.]"

// WithoutOutsideText is a user message as a later turn may see it: the
// blocks marked outside left out (OmittedQuote in their place), the user's
// own text and plain pastes kept. Only the turn that answered the message
// read the passage; kept in the history, it would reach every later turn as
// the user's own words, unlocked. When a marked block cannot be taken out,
// only the stand-in is left.
func WithoutOutsideText(m Message) string {
	if !m.ReadOutside || m.Role != RoleUser {
		return m.Content
	}
	var blocks []MessagePastedText
	if json.Unmarshal(m.PastedTexts, &blocks) != nil {
		return OmittedQuote
	}
	text, matched := StripPastedBlocks(m.Content, blocks)
	parts := []string{}
	if strings.TrimSpace(text) != "" {
		parts = append(parts, text)
	}
	omitted := false
	for i, b := range blocks {
		switch {
		case b.Outside && !matched[i]:
			return OmittedQuote
		case b.Outside:
			omitted = true
		case matched[i]:
			parts = append(parts, b.Text)
		}
	}
	if !omitted {
		// Flagged for another reason than a block: nothing safe to keep.
		return OmittedQuote
	}
	return strings.Join(append(parts, OmittedQuote), "\n\n")
}

// TypedText is what the user typed in a message, without any pasted block
// (a paste can be anyone's text, an idea's passage is outside text). ok is
// false when a block marked outside could not be taken out, or the blocks do
// not decode on a message flagged read_outside: its text may still hold
// outside text, so it must not pass as the user's words.
func TypedText(m Message) (string, bool) {
	var blocks []MessagePastedText
	if len(m.PastedTexts) > 0 {
		if err := json.Unmarshal(m.PastedTexts, &blocks); err != nil {
			return m.Content, !m.ReadOutside
		}
	}
	text, matched := StripPastedBlocks(m.Content, blocks)
	for i, b := range blocks {
		if b.Outside && !matched[i] {
			return "", false
		}
	}
	if m.ReadOutside && !QuotesOutside(m.PastedTexts) {
		// Flagged for another reason than a block: nothing to take out.
		return "", false
	}
	return text, true
}
