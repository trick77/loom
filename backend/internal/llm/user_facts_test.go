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

func factServer(t *testing.T, content, finish string, body *string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if body != nil {
			*body = string(raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + jsonString(content) + `},"finish_reason":"` + finish + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	return mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
}

func jsonString(s string) string {
	var b strings.Builder
	writeJSON(&b, s)
	return strings.TrimSpace(b.String())
}

func TestExtractUserFacts_ParsesOpsAndSendsContext(t *testing.T) {
	var body string
	c := factServer(t, "```json\n{\"ops\":[{\"op\":\"reinforce\",\"id\":\"f1\",\"threads\":[\"T1\"]},{\"op\":\"add\",\"ref\":\"n1\",\"category\":\"personal\",\"content\":\"Lives Bern\",\"long_term\":true}]}\n```", "stop", &body)

	ops, err := c.ExtractUserFacts(context.Background(),
		[]FactInput{{ID: "f1", Category: "work", Content: "Backend dev at Acme", LastSeen: "2026-01-01"}},
		"### T1 (2026-02-01)\nUser: moved to Bern", "- Always answer in metric", []string{"Works at Initech"}, "")
	if err != nil {
		t.Fatalf("ExtractUserFacts: %v", err)
	}
	if len(ops) != 2 || ops[0].Op != "reinforce" || ops[0].Threads[0] != "T1" || !ops[1].Durable || ops[1].Ref != "n1" {
		t.Fatalf("ops = %+v", ops)
	}
	for _, want := range []string{"Backend dev at Acme", "moved to Bern", "Always answer in metric", "Works at Initech", "Do NOT add"} {
		if !strings.Contains(body, want) {
			t.Errorf("request missing %q", want)
		}
	}
}

func TestFactCompletion_TruncatedAndMalformedApplyNothing(t *testing.T) {
	c := factServer(t, `{"ops":[{"op":"add"`, "length", nil)
	if _, err := c.ExtractUserFacts(context.Background(), nil, "User: hi", "", nil, ""); !errors.Is(err, ErrMemoryTruncated) {
		t.Fatalf("truncated err = %v, want ErrMemoryTruncated", err)
	}
	c = factServer(t, "Sure! Here are the facts.", "stop", nil)
	if _, err := c.ConsolidateUserFacts(context.Background(), nil, ""); !errors.Is(err, ErrFactReplyMalformed) {
		t.Fatalf("prose reply err = %v, want ErrFactReplyMalformed", err)
	}
}

func TestConsolidateUserFacts_ParsesMerges(t *testing.T) {
	c := factServer(t, `{"merges":[{"ids":["a","b"],"content":"Climbs, mostly bouldering"}]}`, "stop", nil)
	merges, err := c.ConsolidateUserFacts(context.Background(), []FactInput{{ID: "a"}, {ID: "b"}}, "")
	if err != nil || len(merges) != 1 || len(merges[0].IDs) != 2 {
		t.Fatalf("merges = %+v, %v", merges, err)
	}
}

func TestUserFactsPrompts_GuardLongTermAndDirectives(t *testing.T) {
	for _, want := range []string{"Never retire a fact for being old", "EVERY existing fact", "standing instructions", "superseded_by"} {
		if !strings.Contains(UserFactsSystemPrompt, want) {
			t.Errorf("refresh prompt missing %q", want)
		}
	}
	if !strings.Contains(UserFactsConsolidateSystemPrompt, "never drop information") {
		t.Errorf("consolidate prompt must forbid dropping information")
	}
}
