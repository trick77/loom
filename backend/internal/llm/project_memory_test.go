package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserMemorySystemPromptRequiresTerseFragments(t *testing.T) {
	if strings.Contains(UserMemorySystemPrompt, "single, self-contained sentence") {
		t.Fatalf("user memory prompt still asks for sentence-style memories: %q", UserMemorySystemPrompt)
	}
	for _, want := range []string{
		"terse '- ' fragment lines",
		"Do NOT start facts with \"The user\"",
		"drop filler words",
		"caveman",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

// TestUserMemorySystemPromptIsStructured guards the new sectioned structure:
// work/personal context, a churning "top of mind", and a time-layered brief
// history — all within the budget the prompt divides across its sections.
func TestUserMemorySystemPromptIsStructured(t *testing.T) {
	for _, want := range []string{
		"## Work context",
		"## Personal context",
		"## Top of mind",
		"## Brief history",
		"### Recent months",
		"CHURNS",
		"6000 characters",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing structural marker %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

// TestUserMemorySystemPromptExcludesResponsePreferences guards the layer
// boundary: response-behavior preferences (how to answer) must NOT be recorded in
// derived memory — they belong to the separate user-steered standing-instructions
// layer. This is the fix for "a strong dislike landing in both layers".
func TestUserMemorySystemPromptExcludesResponsePreferences(t *testing.T) {
	for _, want := range []string{
		"never record instructions about how to respond",
		"standing instructions",
		"personal taste",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing boundary marker %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

func TestProjectMemorySystemPromptRequiresTerseFragments(t *testing.T) {
	for _, want := range []string{
		"terse '- ' fragment lines",
		"Do NOT start facts with \"The user\"",
		"drop filler words",
		"caveman",
	} {
		if !strings.Contains(ProjectMemorySystemPrompt, want) {
			t.Fatalf("project memory prompt missing %q:\n%s", want, ProjectMemorySystemPrompt)
		}
	}
}

// TestProjectMemorySystemPromptDefinesFixedSections guards the sectioned-profile
// structure: project memory must request the five fixed markdown headings so the
// output stays a consistent profile shared across the project's chats.
func TestProjectMemorySystemPromptDefinesFixedSections(t *testing.T) {
	for _, want := range []string{
		"## Purpose & context",
		"## Current state",
		"## Key learnings & principles",
		"## Approach & patterns",
		"## Tools & resources",
	} {
		if !strings.Contains(ProjectMemorySystemPrompt, want) {
			t.Fatalf("project memory prompt missing section heading %q:\n%s", want, ProjectMemorySystemPrompt)
		}
	}
}

// TestUserMemorySystemPromptAgesAndMigrates guards two behaviors of the new
// structure: items demote downward through the brief history as they stop
// recurring, and legacy headings are re-bucketed (not carried forward) so
// existing users migrate on their next refresh.
func TestUserMemorySystemPromptAgesAndMigrates(t *testing.T) {
	for _, want := range []string{
		"DEMOTE them one level",
		"Recent months → Earlier context → Long-term background",
		"re-bucket their content",
		"Never carry an old heading forward",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing aging/migration guardrail %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

// TestProjectMemorySystemPromptPrioritizesAndPrunes guards the relaxed-ratchet
// behavior: project memory prioritizes still-in-force facts and actively prunes
// resolved/stale ones instead of keeping everything forever.
func TestProjectMemorySystemPromptPrioritizesAndPrunes(t *testing.T) {
	for _, want := range []string{
		"still in force",
		"curating, not accumulating",
	} {
		if !strings.Contains(ProjectMemorySystemPrompt, want) {
			t.Fatalf("project memory prompt missing prioritization guardrail %q:\n%s", want, ProjectMemorySystemPrompt)
		}
	}
}

// TestApplyMemoryEdit_appliesInstructionInPlace checks that the edit call sends
// the current memory, the instruction, and the style system prompt, and returns
// the model's updated memory.
func TestApplyMemoryEdit_appliesInstructionInPlace(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"- Lives in Zurich\n- Loves climbing"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	out, err := c.ApplyMemoryEdit(context.Background(), "", "- Works at Thoughtworks", "Remember I live in Zurich and love climbing", UserMemorySystemPrompt, "")
	if err != nil {
		t.Fatalf("ApplyMemoryEdit error = %v", err)
	}
	if !strings.Contains(out, "Zurich") {
		t.Errorf("output = %q, want it to contain the model's edited memory", out)
	}
	for _, want := range []string{"Works at Thoughtworks", "Remember I live in Zurich", "## Work context"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %q:\n%s", want, gotBody)
		}
	}
}

// TestApplyMemoryEdit_authoritativeRemoval verifies the prompt frames the
// instruction as authoritative so an explicit removal overrides the retention
// guardrail.
func TestApplyMemoryEdit_authoritativeRemoval(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	out, err := c.ApplyMemoryEdit(context.Background(), "", "- Former baseball player", "Forget my baseball career", UserMemorySystemPrompt, "")
	if err != nil {
		t.Fatalf("ApplyMemoryEdit error = %v", err)
	}
	if out != "" {
		t.Errorf("output = %q, want empty when the model removes the only fact", out)
	}
	if !strings.Contains(strings.ToLower(gotBody), "authoritative") {
		t.Errorf("request body should frame the instruction as authoritative:\n%s", gotBody)
	}
}
