package httpapi

import "testing"

func TestIsTypographyImageRequest(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		// Typography / logo / text — should route to the typography model.
		{"logo noun", "a sleek minimalist logo for a coffee brand", true},
		{"wordmark", "a bold geometric wordmark in serif type", true},
		{"poster", "a vintage travel poster of the alps", true},
		{"infographic", "an infographic about annual rainfall by region", true},
		{"double-quoted text", "a storefront banner that displays \"GRAND OPENING\"", true},
		{"single-quoted span", "a banner reading 'OPEN TODAY' over a cafe", true},
		{"typographic quotes", "a neon sign glowing “LATE NIGHT” in pink", true},
		{"that-says phrase", "a wooden sign that says welcome home", true},
		{"german schriftzug", "ein eleganter schriftzug fuer eine baeckerei", true},
		{"german plakat", "ein retro plakat fuer ein musikfestival", true},

		// Ordinary imagery — must NOT route (stays on the default model).
		{"plain animal", "a red fox in deep snow at dawn", false},
		{"lego transform", "render a lego set from this photo", false},
		{"watercolor scene", "a watercolor mountain landscape at sunset", false},
		{"contraction apostrophe", "a fox that doesn't like the cold winter", false},
		{"possessive apostrophe", "the dog's owner walking through a park", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTypographyImageRequest(tc.prompt); got != tc.want {
				t.Fatalf("isTypographyImageRequest(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}
