package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Regression: the description rides the project-memory refresh and is generated from a
// large transcript, so its completion must NOT borrow the tiny title-sized utility cap
// (utilityMaxCompletionTokens=32) — that truncated the reply to finish_reason=length and
// the description was discarded as empty. It must carry its own larger budget.
func TestGenerateProjectDescription_usesItsOwnLargerTokenBudget(t *testing.T) {
	var gotMaxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
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
	got, err := c.GenerateProjectDescription(context.Background(), "Japan Trip", "user: where to stay in Kyoto?")
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
	got, err := c.GenerateProjectDescription(context.Background(), "Proj", "user: hi")
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
	got, err := c.GenerateProjectDescription(context.Background(), "Proj", "user: hi")
	if err != nil {
		t.Fatalf("GenerateProjectDescription error = %v", err)
	}
	if got != "" {
		t.Errorf("description = %q, want empty when truncated reply has no text", got)
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
