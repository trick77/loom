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
