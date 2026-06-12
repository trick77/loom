package llm

import (
	"strings"
	"testing"
)

func TestUserMemorySystemPromptRequiresTerseFragments(t *testing.T) {
	if strings.Contains(UserMemorySystemPrompt, "single, self-contained sentence") {
		t.Fatalf("user memory prompt still asks for sentence-style memories: %q", UserMemorySystemPrompt)
	}
	for _, want := range []string{
		"terse fragments",
		"Do NOT start facts with \"The user\"",
		"drop filler words",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

func TestProjectMemorySystemPromptRequiresTerseFragments(t *testing.T) {
	for _, want := range []string{
		"terse fragments",
		"Do NOT start facts with \"The user\"",
		"drop filler words",
	} {
		if !strings.Contains(ProjectMemorySystemPrompt, want) {
			t.Fatalf("project memory prompt missing %q:\n%s", want, ProjectMemorySystemPrompt)
		}
	}
}
