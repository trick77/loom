package httpapi

import (
	"strings"
	"testing"
)

// bigCode returns a code body with n lines so size-gate cases are explicit.
func bigCode(n int) string {
	return strings.TrimRight(strings.Repeat("x = 1\n", n), "\n")
}

func fence(lang, body string) string {
	return "```" + lang + "\n" + body + "\n```\n"
}

func TestQualifyingCodeBlocks_GatesByLanguageAndSize(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantCount int
		wantExt   string
	}{
		{"large python qualifies", fence("python", bigCode(60)), 1, "py"},
		{"large bash maps to sh", fence("bash", bigCode(60)), 1, "sh"},
		{"large xml qualifies", fence("xml", strings.Repeat("<a/>\n", 60)), 1, "xml"},
		{"short python snippet skipped", fence("python", "print(1)"), 0, ""},
		{"no language tag skipped", fence("", bigCode(60)), 0, ""},
		{"prose markdown skipped", fence("markdown", bigCode(60)), 0, ""},
		{"json data skipped", fence("json", strings.Repeat("{\"a\":1}\n", 60)), 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualifyingCodeBlocks(tt.content)
			if len(got) != tt.wantCount {
				t.Fatalf("qualifyingCodeBlocks() = %d blocks, want %d", len(got), tt.wantCount)
			}
			if tt.wantCount == 1 && got[0].extension != tt.wantExt {
				t.Fatalf("extension = %q, want %q", got[0].extension, tt.wantExt)
			}
		})
	}
}

func TestQualifyingCodeBlocks_QualifiesByCharCountBelowLineThreshold(t *testing.T) {
	// One very long line: under the line threshold but over the char threshold.
	long := "x = '" + strings.Repeat("a", codeArtifactMinChars) + "'"
	got := qualifyingCodeBlocks(fence("python", long))
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1 (char-count gate)", len(got))
	}
}

func TestQualifyingCodeBlocks_NamesFromPrecedingHeading(t *testing.T) {
	content := "## Server Setup\n\n" + fence("python", bigCode(60))
	got := qualifyingCodeBlocks(content)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got))
	}
	if got[0].filename != "server-setup.py" {
		t.Fatalf("filename = %q, want server-setup.py", got[0].filename)
	}
}

func TestQualifyingCodeBlocks_InterveningProseDetachesHeading(t *testing.T) {
	content := "## Intro\n\nHere is a long explanation paragraph before the code.\n\n" + fence("python", bigCode(60))
	got := qualifyingCodeBlocks(content)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got))
	}
	if got[0].filename != "code-1.py" {
		t.Fatalf("filename = %q, want fallback code-1.py (heading detached by prose)", got[0].filename)
	}
}

func TestQualifyingCodeBlocks_FallbackNameWhenNoHeading(t *testing.T) {
	got := qualifyingCodeBlocks(fence("go", bigCode(60)))
	if len(got) != 1 || got[0].filename != "code-1.go" {
		t.Fatalf("filename = %+v, want code-1.go", got)
	}
}

func TestQualifyingCodeBlocks_DedupsIdenticalBlocks(t *testing.T) {
	block := fence("python", bigCode(60))
	got := qualifyingCodeBlocks(block + "\n" + block)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1 after dedup", len(got))
	}
}

func TestQualifyingCodeBlocks_MultipleDistinctBlocksGetIndexedNames(t *testing.T) {
	content := fence("python", bigCode(60)) + "\n" + fence("go", bigCode(55))
	got := qualifyingCodeBlocks(content)
	if len(got) != 2 {
		t.Fatalf("got %d blocks, want 2", len(got))
	}
	if got[0].filename != "code-1.py" || got[1].filename != "code-2.go" {
		t.Fatalf("filenames = %q, %q", got[0].filename, got[1].filename)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Server Setup":          "server-setup",
		"  Trim **Me**  ":       "trim-me",
		"Already-slug_ok":       "already-slug-ok",
		"###":                   "",
		"Über Größe":            "ber-gr-e",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
