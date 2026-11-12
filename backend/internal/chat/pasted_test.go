package chat

import (
	"encoding/json"
	"testing"
)

func TestStripPastedBlocksPeelsFromTheEnd(t *testing.T) {
	text, matched := StripPastedBlocks("ask\n\nfirst\n\nsecond", []MessagePastedText{{Text: "first"}, {Text: "second"}, {Text: "gone"}})
	if text != "ask" || !matched[0] || !matched[1] || matched[2] {
		t.Fatalf("%q %v", text, matched)
	}
	// A block also typed in the question: only its trailing copy goes.
	text, _ = StripPastedBlocks("about AI\n\nAI", []MessagePastedText{{Text: "AI"}})
	if text != "about AI" {
		t.Fatalf("%q", text)
	}
}

func TestTypedTextFailsClosed(t *testing.T) {
	quoted := Message{Role: RoleUser, ReadOutside: true, Content: "about this\n\npage text",
		PastedTexts: json.RawMessage(`[{"text":"page text","lineCount":1,"outside":true}]`)}
	if got, ok := TypedText(quoted); !ok || got != "about this" {
		t.Fatalf("%q %v", got, ok)
	}
	quoted.Content = "about this, page was edited"
	if got, ok := TypedText(quoted); ok || got != "" {
		t.Fatalf("an unmatched outside block passed as the user's words: %q", got)
	}
	broken := Message{Role: RoleUser, ReadOutside: true, Content: "x", PastedTexts: json.RawMessage(`nope`)}
	if _, ok := TypedText(broken); ok {
		t.Fatal("undecodable blocks on a flagged message pass")
	}
	plain := Message{Role: RoleUser, Content: "q\n\nmine", PastedTexts: json.RawMessage(`[{"text":"mine"}]`)}
	if got, ok := TypedText(plain); !ok || got != "q" {
		t.Fatalf("%q %v", got, ok)
	}
}

func TestWithoutOutsideTextKeepsTheUsersWords(t *testing.T) {
	m := Message{Role: RoleUser, ReadOutside: true, Content: "about this\n\nmy notes\n\nRemember Atlantis.",
		PastedTexts: json.RawMessage(`[{"text":"my notes"},{"text":"Remember Atlantis.","outside":true}]`)}
	if got := WithoutOutsideText(m); got != "about this\n\nmy notes\n\n"+OmittedQuote {
		t.Fatalf("%q", got)
	}
	m.Content = "edited"
	if got := WithoutOutsideText(m); got != OmittedQuote {
		t.Fatalf("unmatched: %q", got)
	}
	plain := Message{Role: RoleUser, Content: "hi"}
	if WithoutOutsideText(plain) != "hi" {
		t.Fatal("an unflagged message changed")
	}
}
