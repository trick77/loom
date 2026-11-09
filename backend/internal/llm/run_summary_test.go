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
	srv := intentServer(t, "Summary: \"Two new listings under your limit, one with a balcony on Nordstrasse.\"", &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	got, err := c.SummarizeRun(context.Background(), "find flats", "## Flats\n- Nordstrasse 12 [2]", "German")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Two new listings under your limit, one with a balcony on Nordstrasse."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	for _, part := range []string{"find flats", "Nordstrasse 12", "German", "at most 200 characters"} {
		if !strings.Contains(body, part) {
			t.Fatalf("request lacks %q: %s", part, body)
		}
	}
}

func TestSummarizeRunFailures(t *testing.T) {
	for name, payload := range map[string]string{
		"cut off": `{"choices":[{"message":{"role":"assistant","content":"Two new"},"finish_reason":"length"}]}`,
		"empty":   `{"choices":[{"message":{"role":"assistant","content":" \"\" "},"finish_reason":"stop"}]}`,
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

func TestUnwrapSummary(t *testing.T) {
	for in, want := range map[string]string{
		"„Zwei neue Wohnungen.“":       "Zwei neue Wohnungen.",
		"«Deux films.»":                "Deux films.",
		"'Quoted.'":                    "Quoted.",
		"Listed by the Joneses'":       "Listed by the Joneses'",
		"summary: Two films.":          "Two films.",
		"Note: the Arena opens at 18.": "Note: the Arena opens at 18.",
	} {
		if got := unwrapSummary(in); got != want {
			t.Errorf("unwrapSummary(%q) = %q, want %q", in, got, want)
		}
	}
}
