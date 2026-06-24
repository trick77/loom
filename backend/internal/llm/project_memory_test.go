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

// TestUserMemorySystemPromptPreservesImportantFacts guards the retention
// guardrail: the summarizer must not silently drop durable, identity-defining
// facts (including favourite things and strong dislikes) to save space.
func TestUserMemorySystemPromptPreservesImportantFacts(t *testing.T) {
	for _, want := range []string{
		"favourite things",
		"hates or loathes",
		"compress the wording, not the facts",
	} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Fatalf("user memory prompt missing retention guardrail %q:\n%s", want, UserMemorySystemPrompt)
		}
	}
}

func TestProjectMemorySystemPromptPreservesImportantFacts(t *testing.T) {
	if !strings.Contains(ProjectMemorySystemPrompt, "compress the wording, not the facts") {
		t.Fatalf("project memory prompt missing retention guardrail:\n%s", ProjectMemorySystemPrompt)
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
	for _, want := range []string{"Works at Thoughtworks", "Remember I live in Zurich", "terse fragments"} {
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
