package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression: the description rides the project-memory refresh and is generated from a
// large transcript, so its completion must NOT borrow the tiny title-sized utility cap
// (utilityMaxCompletionTokens=32) — that truncated the reply to finish_reason=length and
// the description was discarded as empty. It must carry its own larger budget.
func TestGenerateProjectDescription_usesItsOwnLargerTokenBudget(t *testing.T) {
	var gotMaxTokens int
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		var req struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		}
		_ = json.Unmarshal(body, &req)
		gotMaxTokens = req.MaxCompletionTokens
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Trip planning for a week in Japan"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	got, err := c.GenerateProjectDescription(context.Background(), "Japan Trip", []string{"Where to stay in Kyoto", "Bullet train passes"}, "")
	if err != nil {
		t.Fatalf("GenerateProjectDescription error = %v", err)
	}
	if gotMaxTokens != projectDescriptionMaxCompletionTokens {
		t.Errorf("max_completion_tokens = %d, want %d (must not reuse the %d-token title cap)",
			gotMaxTokens, projectDescriptionMaxCompletionTokens, utilityMaxCompletionTokens)
	}
	if got != "Trip planning for a week in Japan." {
		t.Errorf("description = %q, want cleaned model output", got)
	}
	// The whole point of the redesign: the prompt summarizes the thread TITLES. Assert
	// they (and the titles framing) actually reach the request — a builder that dropped
	// them or still emitted transcript framing would otherwise pass every other test.
	for _, want := range []string{"Thread titles in this project", "Where to stay in Kyoto", "Bullet train passes"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %q:\n%s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, "Early conversation") {
		t.Errorf("request body still uses the old transcript framing:\n%s", gotBody)
	}
}

// A reply truncated by the token cap (finish_reason=length) is salvaged into a usable
// description — the dangling partial last word is dropped, the rest is kept. A clipped
// one-liner beats a permanently empty description (and an empty return would make the
// backfill retry forever).
func TestGenerateProjectDescription_salvagesLengthTruncatedReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"A very long descrip"},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	got, err := c.GenerateProjectDescription(context.Background(), "Proj", []string{"hi"}, "")
	if err != nil {
		t.Fatalf("GenerateProjectDescription error = %v", err)
	}
	if got != "A very long." {
		t.Errorf("description = %q, want salvaged partial %q on finish_reason=length", got, "A very long.")
	}
}

// A truncation with no salvageable text (only a single partial word) returns empty
// rather than a meaningless token.
func TestGenerateProjectDescription_lengthTruncatedSingleWordIsKept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	got, err := c.GenerateProjectDescription(context.Background(), "Proj", []string{"hi"}, "")
	if err != nil {
		t.Fatalf("GenerateProjectDescription error = %v", err)
	}
	if got != "" {
		t.Errorf("description = %q, want empty when truncated reply has no text", got)
	}
}

// The user's complaint: descriptions sometimes come out too large. The prompt asks for
// ≤160 chars, but a model that ignores it must still be hard-capped in code on a word
// boundary so the stored description never exceeds projectDescriptionMaxChars.
func TestGenerateProjectDescription_hardCapsOversizedReply(t *testing.T) {
	long := "This project covers an extremely wide and rambling range of unrelated topics including travel planning, tax accounting, garden landscaping, marathon training schedules, and assorted miscellaneous experiments that go on and on well past any reasonable length"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": long}, "finish_reason": "stop"}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	got, err := c.GenerateProjectDescription(context.Background(), "Everything", []string{"a", "b"}, "")
	if err != nil {
		t.Fatalf("GenerateProjectDescription error = %v", err)
	}
	if n := len([]rune(got)); n > projectDescriptionMaxChars {
		t.Errorf("description length = %d runes, want <= %d; got %q", n, projectDescriptionMaxChars, got)
	}
	if strings.HasSuffix(got, " .") || !strings.HasSuffix(got, ".") {
		t.Errorf("description = %q, want a clean word-boundary cut ending in a period", got)
	}
}

func TestCleanProjectDescription(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no trailing period", "A chat app", "A chat app."},
		{"already has period", "A chat app.", "A chat app."},
		{"ellipsis collapses to single period", "A chat app...", "A chat app."},
		{"exclamation untouched", "Wow!", "Wow!"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanProjectDescription(tc.in); got != tc.want {
				t.Errorf("cleanProjectDescription(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
