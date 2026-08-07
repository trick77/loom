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

// A title the user never typed as a title — their prompt, or the titling model's
// output — is what the sidebar and the thread header show, so it reads as a
// sentence.
func TestCapitalizeThreadTitle(t *testing.T) {
	cases := map[string]string{
		"why is the sky blue?": "Why is the sky blue?",
		// A second uppercase letter means the lowercase first one is deliberate.
		"iPhone battery drains overnight": "iPhone battery drains overnight",
		"eBay export":                     "eBay export",
		// Already capitalized, or not a letter at all: untouched.
		"Blue Sky Explanation":  "Blue Sky Explanation",
		"42 ways to fold a map": "42 ways to fold a map",
		"":                      "",
		// Non-ASCII lowercase is capitalized too (unicode.ToUpper, unlike the
		// SQLite backfill).
		"über die wolken": "Über die wolken",
	}
	for in, want := range cases {
		if got := CapitalizeThreadTitle(in); got != want {
			t.Errorf("CapitalizeThreadTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// Normalizing alone must never change case: it also runs on renames, where the
// title is the user's own text and has to stay exactly as they typed it.
func TestNormalizeThreadTitleLeavesCaseAlone(t *testing.T) {
	cases := map[string]string{
		"ffmpeg notes":            "ffmpeg notes",
		"why is the sky blue?":    "why is the sky blue?",
		`"why is the sky blue?"`:  "why is the sky blue?",
		"## why me":               "why me",
		"  npm   install  fails ": "npm install fails",
	}
	for in, want := range cases {
		if got := NormalizeThreadTitle(in); got != want {
			t.Errorf("NormalizeThreadTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
