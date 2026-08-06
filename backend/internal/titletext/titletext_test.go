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

func TestDriftsFromSourceScripts(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		sources []string
		want    bool
	}{
		{
			// The reported case: an English question titled in Chinese.
			name:    "cjk title from a latin source drifts",
			title:   "Kasia Knez婚姻查询",
			sources: []string{"kasia newyadoma is married to whom?"},
			want:    true,
		},
		{
			name:    "latin title from a latin source",
			title:   "Blue Sky Explanation",
			sources: []string{"Explain why the sky is blue"},
			want:    false,
		},
		{
			// A user writing Chinese must still get a Chinese title.
			name:    "han title from a han source",
			title:   "天空颜色解释",
			sources: []string{"为什么天空是蓝色的？"},
			want:    false,
		},
		{
			// Han, Hiragana and Katakana share one group: ordinary Japanese
			// mixes them, so a Han-only title over a kana source is not drift.
			name:    "han title from a hiragana source",
			title:   "俳句解説",
			sources: []string{"はいくについておしえて"},
			want:    false,
		},
		{
			name:    "cyrillic title from a latin source drifts",
			title:   "Объяснение неба",
			sources: []string{"Explain why the sky is blue"},
			want:    true,
		},
		{
			// Non-letters carry no script identity.
			name:    "punctuation digits and emoji are ignored",
			title:   `"Top 10" — Results 🎉`,
			sources: []string{"give me the top 10 results"},
			want:    false,
		},
		{
			// The assistant answer is a source too, so a term it introduced
			// in its own script is legitimate.
			name:    "katakana term present in a source",
			title:   "Explaining カラオケ etiquette",
			sources: []string{"what is karaoke etiquette", "カラオケ is the Japanese term."},
			want:    false,
		},
		{
			name:    "empty title never drifts",
			title:   "",
			sources: []string{"anything at all"},
			want:    false,
		},
		{
			// With nothing to compare against, every recognized script drifts —
			// including Latin. Callers must always pass their source text.
			name:  "any recognized script drifts with no sources",
			title: "Marriage Lookup",
			want:  true,
		},
		{
			name:  "unrecognized script does not drift even with no sources",
			title: "Բացատրություն",
			want:  false,
		},
		{
			// Unrecognized scripts are allowed rather than guessed at.
			name:    "unlisted script is allowed",
			title:   "Բացատրություն",
			sources: []string{"Explain why the sky is blue"},
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DriftsFromSourceScripts(tc.title, tc.sources...); got != tc.want {
				t.Errorf("DriftsFromSourceScripts(%q, %q) = %v, want %v", tc.title, tc.sources, got, tc.want)
			}
		})
	}
}
