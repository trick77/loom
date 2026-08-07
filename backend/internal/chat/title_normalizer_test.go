package chat

import "testing"

func TestNormalizeThreadTitleQuoteHandling(t *testing.T) {
	cases := map[string]string{
		// Real bug: the dangling typographic closing quote and missing opening
		// quote must both be fixed into a balanced, straight-quoted pair.
		"\"Healing” by Evanescence": `"Healing" by Evanescence`,
		"“Healing” by Evanescence":  `"Healing" by Evanescence`,
		// Fully wrapped titles are still unwrapped (no regression).
		`"Blue Sky Explanation"`: "Blue Sky Explanation",
		"“Blue Sky Explanation”": "Blue Sky Explanation",
		// Plain title untouched.
		"Blue Sky Explanation": "Blue Sky Explanation",
	}
	for in, want := range cases {
		if got := NormalizeThreadTitle(in); got != want {
			t.Errorf("NormalizeThreadTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// A title taken straight from the user's prompt is what the sidebar and the
// thread header show until the generated one arrives, so it reads as a sentence.
func TestNormalizeThreadTitleCapitalization(t *testing.T) {
	cases := map[string]string{
		"why is the sky blue?": "Why is the sky blue?",
		// A second uppercase letter means the lowercase first one is deliberate.
		"iPhone battery drains overnight": "iPhone battery drains overnight",
		"eBay export":                     "eBay export",
		// Already capitalized, or not a letter at all: untouched.
		"Blue Sky Explanation":  "Blue Sky Explanation",
		"42 ways to fold a map": "42 ways to fold a map",
		// Non-ASCII lowercase is capitalized too (unicode.ToUpper, unlike the
		// SQLite backfill).
		"über die wolken": "Über die wolken",
		// Capitalization happens after the quote/markdown/whitespace cleanup, so
		// it sees the real first character.
		`"why is the sky blue?"`: "Why is the sky blue?",
		"## why me":              "Why me",
	}
	for in, want := range cases {
		if got := NormalizeThreadTitle(in); got != want {
			t.Errorf("NormalizeThreadTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
