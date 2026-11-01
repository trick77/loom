package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	out, err := c.ApplyMemoryEdit(context.Background(), "", "- Works at Thoughtworks", "Remember I live in Zurich and love climbing", ProjectMemorySystemPrompt, "")
	if err != nil {
		t.Fatalf("ApplyMemoryEdit error = %v", err)
	}
	if !strings.Contains(out, "Zurich") {
		t.Errorf("output = %q, want it to contain the model's edited memory", out)
	}
	for _, want := range []string{"Works at Thoughtworks", "Remember I live in Zurich", "## Current state"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %q:\n%s", want, gotBody)
		}
	}
}

// A memory reply cut off by the token cap must never be stored: the tail of a
// sectioned memory is its oldest context, and a truncated rewrite silently
// replaced a whole user memory with a 90-token fragment in prod.
func TestMemoryCompletion_TruncatedReplyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"## Work context\n- Backend dev at"},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())

	if out, err := c.GenerateMemory(context.Background(), "", "- prior", "User: hi", "", ProjectMemorySystemPrompt, ""); !errors.Is(err, ErrMemoryTruncated) {
		t.Fatalf("GenerateMemory = %q, %v; want ErrMemoryTruncated", out, err)
	}
	if out, err := c.ApplyMemoryEdit(context.Background(), "", "- prior", "forget X", ProjectMemorySystemPrompt, ""); !errors.Is(err, ErrMemoryTruncated) {
		t.Fatalf("ApplyMemoryEdit = %q, %v; want ErrMemoryTruncated", out, err)
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

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	out, err := c.ApplyMemoryEdit(context.Background(), "", "- Former baseball player", "Forget my baseball career", ProjectMemorySystemPrompt, "")
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
