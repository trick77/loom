package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/classifier"
)

// The four short gates are the calls a turn blocks on, so what they return when
// the endpoint misbehaves IS the product behaviour: every one of them has to
// degrade to a usable value rather than propagate a failure into the turn. These
// tests pin that per gate across the three ways the endpoint can disappoint —
// an error status, an undecodable body, and a well-formed reply with no choices
// (which the endpoint does emit when it drops a request).

func gateServer(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{BaseURL: server.URL}, server.Client())
}

const emptyChoicesBody = `{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":0,"total_tokens":7}}`

func TestClassifyImageIntentDegradesToNone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "upstream error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantErr: true},
		{name: "undecodable body", status: http.StatusOK, body: `not json`, wantErr: true},
		{name: "no choices", status: http.StatusOK, body: emptyChoicesBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent, err := gateServer(t, tc.status, tc.body).ClassifyImageIntent(context.Background(), "draw a fox", false, false)
			if tc.wantErr && err == nil {
				t.Fatal("expected an error to be reported to the caller")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if intent.Action != ImageIntentNone {
				t.Fatalf("action = %q, want %q — a failed gate must never route to the image tool", intent.Action, ImageIntentNone)
			}
		})
	}
}

func TestClassifyThreadDegradesToGeneral(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "upstream error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantErr: true},
		{name: "undecodable body", status: http.StatusOK, body: `not json`, wantErr: true},
		{name: "no choices", status: http.StatusOK, body: emptyChoicesBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			category, err := gateServer(t, tc.status, tc.body).ClassifyThread(context.Background(), "how do I sort a slice?")
			if tc.wantErr && err == nil {
				t.Fatal("expected an error to be reported to the caller")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if category != string(classifier.General) {
				t.Fatalf("category = %q, want %q", category, classifier.General)
			}
		})
	}
}

func TestGenerateThreadTitleDegrades(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantErr   bool
		wantTitle string
	}{
		{name: "upstream error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantErr: true},
		{name: "undecodable body", status: http.StatusOK, body: `not json`, wantErr: true},
		// No choices is not an error: the thread gets the placeholder title and the
		// turn proceeds, rather than failing over a cosmetic label.
		{name: "no choices", status: http.StatusOK, body: emptyChoicesBody, wantTitle: "New thread"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title, err := gateServer(t, tc.status, tc.body).GenerateThreadTitle(context.Background(), "hello", "", "")
			if tc.wantErr && err == nil {
				t.Fatal("expected an error to be reported to the caller")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if title != tc.wantTitle {
				t.Fatalf("title = %q, want %q", title, tc.wantTitle)
			}
		})
	}
}

func TestGenerateReasoningTitleDegradesToEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "upstream error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantErr: true},
		{name: "undecodable body", status: http.StatusOK, body: `not json`, wantErr: true},
		{name: "no choices", status: http.StatusOK, body: emptyChoicesBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title, err := gateServer(t, tc.status, tc.body).GenerateReasoningTitle(context.Background(), "some reasoning", "")
			if tc.wantErr && err == nil {
				t.Fatal("expected an error to be reported to the caller")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Empty means "no label"; the caller falls back to its own heuristic.
			if strings.TrimSpace(title) != "" {
				t.Fatalf("title = %q, want empty", title)
			}
		})
	}
}
