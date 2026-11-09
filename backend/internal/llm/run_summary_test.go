package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSummarizeRun(t *testing.T) {
	var body string
	srv := intentServer(t, "\"Two new listings under your limit [1], one with a balcony on Nordstrasse [2].\"", &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	got, err := c.SummarizeRun(context.Background(), "find flats", "## Flats\n- Nordstrasse 12 [2]", "German")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Two new listings under your limit, one with a balcony on Nordstrasse."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	for _, part := range []string{"find flats", "Nordstrasse 12", "German"} {
		if !strings.Contains(body, part) {
			t.Fatalf("request lacks %q: %s", part, body)
		}
	}
}

func TestSummarizeRunFailures(t *testing.T) {
	for name, payload := range map[string]string{
		"cut off": `{"choices":[{"message":{"role":"assistant","content":"Two new"},"finish_reason":"length"}]}`,
		"empty":   `{"choices":[{"message":{"role":"assistant","content":"  [1] "},"finish_reason":"stop"}]}`,
		"none":    `{"choices":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(payload))
			}))
			defer srv.Close()
			c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
			if got, err := c.SummarizeRun(context.Background(), "t", "a", ""); err == nil {
				t.Fatalf("summary = %q, want an error", got)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if _, err := c.SummarizeRun(context.Background(), "t", "a", ""); err == nil {
		t.Fatal("a failing endpoint gave no error")
	}
}

func TestCleanRunSummaryCapsLength(t *testing.T) {
	long := strings.Repeat("word ", 100)
	got := cleanRunSummary(long)
	if n := len([]rune(got)); n > runSummaryMaxChars {
		t.Fatalf("length = %d, want <= %d", n, runSummaryMaxChars)
	}
	if !strings.HasSuffix(got, "word…") {
		t.Fatalf("summary = %q, want a whole last word and an ellipsis", got)
	}
}
