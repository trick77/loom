package titletext

import "testing"

func TestNormalizeQuotes(t *testing.T) {
	cases := map[string]string{
		"“Healing” by Evanescence": `"Healing" by Evanescence`,
		"‘Cats’":                   "'Cats'",
		`"already straight"`:       `"already straight"`,
		"no quotes here":           "no quotes here",
	}
	for in, want := range cases {
		if got := NormalizeQuotes(in); got != want {
			t.Errorf("NormalizeQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripWrappingQuotes(t *testing.T) {
	cases := map[string]string{
		`"Blue Sky Explanation"`:   "Blue Sky Explanation",
		`'Cats'`:                   "Cats",
		`""Nested""`:               "Nested",
		`"Healing" by Evanescence`: `"Healing" by Evanescence`, // dangling-quote guard: not a wrapping pair
		`bare title`:               "bare title",
		`"`:                        `"`,
	}
	for in, want := range cases {
		if got := StripWrappingQuotes(in); got != want {
			t.Errorf("StripWrappingQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}
