package classifier

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]Category{
		"coding":              Coding,
		"knowledge_discovery": KnowledgeDiscovery,
		"general":             General,
		"":                    General,
		"not_a_category":      General,
		"Coding":              General, // case-sensitive: only exact lowercase values are valid
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := map[string]Category{
		"coding":                    Coding,
		"\"coding\"":                Coding, // wrapping quotes
		"coding.":                   Coding, // trailing punctuation
		"  coding  ":                Coding, // surrounding whitespace
		"Coding":                    Coding, // case-insensitive
		"The category is coding":    Coding, // surrounding prose
		"knowledge_discovery":       KnowledgeDiscovery,
		"url_lookup":                URLLookup, // underscore preserved
		"general":                   General,
		"":                          General, // empty
		"   ":                       General, // whitespace only
		"definitely not a category": General, // unknown words
		"encoding":                  General, // must not partial-match "coding"
	}
	for in, want := range cases {
		if got := Match(in); got != want {
			t.Errorf("Match(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlock(t *testing.T) {
	// Unknown values inject nothing.
	if Block("not_a_category") != "" {
		t.Errorf("Block(unknown) = %q, want empty", Block("not_a_category"))
	}
	// Every catalog category, including General, injects a non-empty block.
	for _, c := range []Category{KnowledgeDiscovery, Coding, CookingRecipes, Weather, Translation, Health, EntertainmentRecs, LocalInfo, FinanceInvesting, URLLookup, General} {
		if Block(string(c)) == "" {
			t.Errorf("Block(%q) is empty, want a directive", c)
		}
	}
}

func TestValuesAndPromptGuideCoverEveryCategory(t *testing.T) {
	if len(Values()) != 22 {
		t.Fatalf("Values() has %d entries, want 22", len(Values()))
	}
	guide := PromptGuide()
	for _, c := range Values() {
		// ImageGeneration is hidden: a valid category, but deliberately omitted
		// from the classifying model's menu.
		if c == ImageGeneration {
			if contains(guide, string(c)) {
				t.Errorf("PromptGuide() must not offer hidden category %q", c)
			}
			continue
		}
		if !contains(guide, string(c)) {
			t.Errorf("PromptGuide() missing category %q", c)
		}
	}
}

func TestPromptGuideExcludes(t *testing.T) {
	guide := PromptGuide(URLLookup)
	// The excluded category's menu line is withheld for this call only. Probe for
	// the "- value:" line, not the bare value, so a gloss merely mentioning the
	// category by name would not false-positive.
	if contains(guide, "- "+string(URLLookup)+":") {
		t.Errorf("PromptGuide(URLLookup) must not offer %q", URLLookup)
	}
	// Exclusion is per-call, not a validity change: the category stays valid.
	if !Valid(string(URLLookup)) {
		t.Errorf("Valid(%q) = false, want true (exclusion must not invalidate)", URLLookup)
	}
	// Hidden categories stay omitted and everything else stays offered.
	if contains(guide, string(ImageGeneration)) {
		t.Errorf("PromptGuide(URLLookup) must not offer hidden category %q", ImageGeneration)
	}
	for _, c := range Values() {
		if c == URLLookup || c == ImageGeneration {
			continue
		}
		if !contains(guide, string(c)) {
			t.Errorf("PromptGuide(URLLookup) missing category %q", c)
		}
	}
}

func TestImageGenerationIsHiddenButValid(t *testing.T) {
	// A valid category so a persisted "image_generation" is never coerced away.
	if !Valid(string(ImageGeneration)) {
		t.Errorf("Valid(%q) = false, want true", ImageGeneration)
	}
	if got := Normalize(string(ImageGeneration)); got != ImageGeneration {
		t.Errorf("Normalize(%q) = %q, want %q", ImageGeneration, got, ImageGeneration)
	}
	// It injects no block — the image path ignores the classifier block anyway.
	if got := Block(string(ImageGeneration)); got != "" {
		t.Errorf("Block(%q) = %q, want empty", ImageGeneration, got)
	}
	// And it is never offered to the classifying model.
	if contains(PromptGuide(), string(ImageGeneration)) {
		t.Errorf("PromptGuide() must not contain hidden category %q", ImageGeneration)
	}
	// Match never returns a hidden category, even if the reply names it: the model
	// is never shown it, so such a token is a hallucination, not a choice.
	if got := Match(string(ImageGeneration)); got != General {
		t.Errorf("Match(%q) = %q, want General (hidden categories are not matchable)", ImageGeneration, got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
