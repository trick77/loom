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

func TestBlock(t *testing.T) {
	// General and unknown values inject nothing.
	if Block(string(General)) != "" {
		t.Errorf("Block(general) = %q, want empty", Block(string(General)))
	}
	if Block("not_a_category") != "" {
		t.Errorf("Block(unknown) = %q, want empty", Block("not_a_category"))
	}
	// Enrichment and utility categories inject a non-empty block.
	for _, c := range []Category{KnowledgeDiscovery, Coding, CookingRecipes, Weather, Translation, URLLookup} {
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
