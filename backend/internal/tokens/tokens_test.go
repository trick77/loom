package tokens

import "testing"

func TestEstimate(t *testing.T) {
	cases := map[string]int{"": 0, "a": 1, "abcd": 1, "abcde": 2, "äöüß": 1}
	for in, want := range cases {
		if got := Estimate(in); got != want {
			t.Errorf("Estimate(%q) = %d, want %d", in, got, want)
		}
	}
}
