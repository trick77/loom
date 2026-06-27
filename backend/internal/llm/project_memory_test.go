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

// TestUserMemorySystemPromptIsStructured guards the Core/Current-focus/Style
// structure: a protected identity section, a capped churning section so transient
// work ages out instead of piling up, and a Style section for response steering —
// all within a 2000-character budget.
func TestUserMemorySystemPromptIsStructured(t *testing.T) {
	for _, want := range []string{
		"## Core",
		"## Current focus",
		"## Style",
		"at most 10 items",
		"CHURNS",
		"2000 characters",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing structural marker %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

// TestUserMemorySystemPromptCapturesResponsePreferences guards the
// behaviour-steering intent: the Style section must capture how the user wants to
// be answered (length/tone/format) inferred from their feedback.
func TestUserMemorySystemPromptCapturesResponsePreferences(t *testing.T) {
	for _, want := range []string{
		"## Style",
		"wants the assistant to respond",
		"feedback",
		"prefers concise answers",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing steering marker %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

func TestProjectMemorySystemPromptRequiresTerseFragments(t *testing.T) {
	for _, want := range []string{
		"terse fragments",
		"Do NOT start facts with \"The user\"",
		"drop filler words",
		"caveman",
	} {
		if !strings.Contains(ProjectMemorySystemPrompt, want) {
			t.Fatalf("project memory prompt missing %q:\n%s", want, ProjectMemorySystemPrompt)
		}
	}
}

// TestUserMemorySystemPromptPreservesImportantFacts guards the retention
// guardrail: durable, identity-defining Core facts (including favourite things
// and strong dislikes) must be protected from being dropped to save space, even
// as the Current-focus section churns.
func TestUserMemorySystemPromptPreservesImportantFacts(t *testing.T) {
	for _, want := range []string{
		"favourite things",
		"hate or loathe",
		"never drop one to save space",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing retention guardrail %q:\n%s", want, UserMemorySystemPrompt)
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
	out, err := c.ApplyMemoryEdit(context.Background(), "", "- Works at Thoughtworks", "Remember I live in Zurich and love climbing", UserMemorySystemPrompt)
	if err != nil {
		t.Fatalf("ApplyMemoryEdit error = %v", err)
	}
	if !strings.Contains(out, "Zurich") {
		t.Errorf("output = %q, want it to contain the model's edited memory", out)
	}
	for _, want := range []string{"Works at Thoughtworks", "Remember I live in Zurich", "## Current focus"} {
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
	out, err := c.ApplyMemoryEdit(context.Background(), "", "- Former baseball player", "Forget my baseball career", UserMemorySystemPrompt)
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
