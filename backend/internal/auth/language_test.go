package auth

import "testing"

func TestResponseLanguageName(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"auto":    "",
		"AUTO":    "",
		"de":      "German",
		"en":      "English",
		"Klingon": "Klingon",
	}
	for value, want := range cases {
		if got := (User{ResponseLanguage: value}).ResponseLanguageName(); got != want {
			t.Errorf("ResponseLanguageName(%q) = %q, want %q", value, got, want)
		}
	}
}
