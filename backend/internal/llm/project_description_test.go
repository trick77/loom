package llm

import "testing"

func TestCleanProjectDescription(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no trailing period", "A chat app", "A chat app."},
		{"already has period", "A chat app.", "A chat app."},
		{"ellipsis collapses to single period", "A chat app...", "A chat app."},
		{"exclamation untouched", "Wow!", "Wow!"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanProjectDescription(tc.in); got != tc.want {
				t.Errorf("cleanProjectDescription(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
