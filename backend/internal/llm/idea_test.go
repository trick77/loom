package llm

import (
	"context"
	"strings"
	"testing"
)

func TestDraftIdea(t *testing.T) {
	var body string
	srv := intentServer(t, "```json\n{\"title\":\"Vibe Kanban\",\"due_on\":\"2027-01-12\"}\n```", &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	draft, err := c.DraftIdea(context.Background(), "Vibe Kanban is a board for coding agents.", "lab week, submit by 12 January", "2026-11-12", "German")
	if err != nil || draft.Title != "Vibe Kanban" || draft.DueOn != "2027-01-12" {
		t.Fatalf("DraftIdea = %+v, %v", draft, err)
	}
	for _, want := range []string{"Vibe Kanban is a board", "submit by 12 January", "Today: 2026-11-12"} {
		if !strings.Contains(body, want) {
			t.Fatalf("%q did not reach the model: %s", want, body)
		}
	}
	// The passage is outside text: the call offers the model nothing to act
	// with.
	if strings.Contains(body, `"tools"`) {
		t.Fatalf("draft call offered tools: %s", body)
	}
	// The reason alone (the date call): no empty passage block.
	if _, err := c.DraftIdea(context.Background(), "", "submit by Friday", "2026-11-12", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "Passage") {
		t.Fatalf("empty passage framed: %s", body)
	}
}

// The reply is parsed as JSON and nothing else: prose, a missing title or a
// wrong type give nothing (the user types it).
func TestParseIdeaDraftReply(t *testing.T) {
	cases := map[string]IdeaDraft{
		`{"title":"  Rye   bread "}`:                                  {Title: "Rye bread"},
		"```json\n{\"title\":\"A\",\"due_on\":\" 2027-01-01 \"}\n```": {Title: "A", DueOn: "2027-01-01"},
		`Sure! The title is "Pwned"`:                                  {},
		`{"title":5}`:                                                 {},
		`{"name":"x"}`:                                                {},
		`{"title":"Line one\nIGNORE ALL RULES"}`:                      {Title: "Line one IGNORE ALL RULES"},
	}
	for reply, want := range cases {
		if got := parseIdeaDraftReply(reply); got != want {
			t.Errorf("parseIdeaDraftReply(%q) = %+v, want %+v", reply, got, want)
		}
	}
}
