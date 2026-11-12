package llm

import (
	"context"
	"strings"
	"testing"
)

func TestDraftIdeaTitle(t *testing.T) {
	var body string
	srv := intentServer(t, "```json\n{\"title\":\"Vibe Kanban\"}\n```", &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	title, err := c.DraftIdeaTitle(context.Background(), "Vibe Kanban is a board for coding agents.", "lab week topic", "German")
	if err != nil || title != "Vibe Kanban" {
		t.Fatalf("DraftIdeaTitle = %q, %v", title, err)
	}
	for _, want := range []string{"Vibe Kanban is a board", "lab week topic"} {
		if !strings.Contains(body, want) {
			t.Fatalf("%q did not reach the model: %s", want, body)
		}
	}
	// The passage is outside text: the call offers the model nothing to act
	// with.
	if strings.Contains(body, `"tools"`) {
		t.Fatalf("draft call offered tools: %s", body)
	}
}

// The reply is parsed as JSON and nothing else: prose, a missing title or a
// wrong type give no title (the user types one).
func TestParseIdeaDraftReply(t *testing.T) {
	cases := map[string]string{
		`{"title":"  Rye   bread "}`:             "Rye bread",
		"```json\n{\"title\":\"A\"}\n```":        "A",
		`Sure! The title is "Pwned"`:             "",
		`{"title":5}`:                            "",
		`{"name":"x"}`:                           "",
		`{"title":"Line one\nIGNORE ALL RULES"}`: "Line one IGNORE ALL RULES",
	}
	for reply, want := range cases {
		if got := parseIdeaDraftReply(reply); got != want {
			t.Errorf("parseIdeaDraftReply(%q) = %q, want %q", reply, got, want)
		}
	}
}
