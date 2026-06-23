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
		"\"coding\"":                Coding,         // wrapping quotes
		"coding.":                   Coding,         // trailing punctuation
		"  coding  ":                Coding,         // surrounding whitespace
		"Coding":                    Coding,         // case-insensitive
		"The category is coding":    Coding,         // surrounding prose
		"knowledge_discovery":       KnowledgeDiscovery,
		"url_lookup":                URLLookup,      // underscore preserved
		"general":                   General,
		"":                          General,        // empty
		"   ":                       General,        // whitespace only
		"definitely not a category": General,        // unknown words
		"encoding":                  General,        // must not partial-match "coding"
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
	for _, c := range []Category{KnowledgeDiscovery, Coding, CookingRecipes, Weather, Translation, URLLookup, General} {
		if Block(string(c)) == "" {
			t.Errorf("Block(%q) is empty, want a directive", c)
		}
	}
}

func TestValuesAndPromptGuideCoverEveryCategory(t *testing.T) {
	if len(Values()) != 17 {
		t.Fatalf("Values() has %d entries, want 17", len(Values()))
	}
	guide := PromptGuide()
	for _, c := range Values() {
		if !contains(guide, string(c)) {
			t.Errorf("PromptGuide() missing category %q", c)
		}
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
