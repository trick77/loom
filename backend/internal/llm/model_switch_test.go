package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trick77/toolkit/llmwire"
	"github.com/trick77/toolkit/llmwire/llmwiretest"
)

func switchHistory() []Message {
	return []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1", Model: "chat"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2", Model: "deep"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a3", Model: "chat"},
		{Role: "user", Content: "q4"},
	}
}

// Answers another model wrote are named by their place in the thread, so the
// model on duty can tell them from its own.
func TestSwitchNote_NamesTheOtherModelsAnswers(t *testing.T) {
	note := switchNote(switchHistory(), "deep")
	if !strings.Contains(note, "answers 1 and 3 in this thread") {
		t.Fatalf("note = %q, want it to name answers 1 and 3", note)
	}
	if strings.Contains(note, "chat") || strings.Contains(note, "deep") {
		t.Fatalf("note = %q names a model", note)
	}
	if got := switchNote(switchHistory(), "chat"); !strings.Contains(got, "answer 2 in this thread") {
		t.Fatalf("note = %q, want it to name answer 2 alone", got)
	}
}

func TestSwitchNote_NoneWithoutASwitch(t *testing.T) {
	same := []Message{{Role: "system"}, {Role: "assistant", Content: "a", Model: "chat"}, {Role: "user", Content: "q"}}
	if got := switchNote(same, "chat"); got != "" {
		t.Fatalf("same model: note %q, want none", got)
	}
	// An answer with no recorded model (old rows, incognito) is the model's
	// own, as before.
	unknown := []Message{{Role: "system"}, {Role: "assistant", Content: "a"}, {Role: "user", Content: "q"}}
	if got := switchNote(unknown, "deep"); got != "" {
		t.Fatalf("unrecorded model: note %q, want none", got)
	}
	// A tool round of this turn carries no model and no prose: never counted.
	round := []Message{{Role: "system"}, {Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []ToolCall{{ID: "c"}}}}
	if got := switchNote(round, "deep"); got != "" {
		t.Fatalf("tool round: note %q, want none", got)
	}
}

// Rows of one answer stored apart (a turn split for a queued message) are one
// answer: the numbers match what the model reads.
func TestSwitchNote_CountsAdjacentRowsAsOneAnswer(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1 part", Model: "chat"},
		{Role: "assistant", Content: "a1 rest", Model: "chat"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2", Model: "deep"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a3", Model: "chat"},
		{Role: "user", Content: "q4"},
	}
	if got := switchNote(history, "deep"); !strings.Contains(got, "answers 1 and 3 in") {
		t.Fatalf("note = %q, want answers 1 and 3", got)
	}
}

// A long thread folds runs into ranges instead of listing every answer.
func TestSwitchNote_FoldsRuns(t *testing.T) {
	history := []Message{}
	for i := range 40 {
		model := "chat"
		if i == 38 {
			model = "deep"
		}
		history = append(history, Message{Role: "user", Content: "q"}, Message{Role: "assistant", Content: "a", Model: model})
	}
	history = append(history, Message{Role: "user", Content: "q"})
	if got := switchNote(history, "deep"); !strings.Contains(got, "answers 1-38 and 40 in") {
		t.Fatalf("note = %q, want answers 1-38 and 40", got)
	}
}

// The note rides on the newest user message, never on the system prompt (the
// cached prefix) or an answer; a picture turn gets it as a text part.
func TestWithSwitchNote_GoesOnTheLastUserMessage(t *testing.T) {
	history := switchHistory()
	wire := withSwitchNote(toWireMessages(history), history, "deep")
	if wire[0].Text != "sys" {
		t.Fatalf("system = %q, want it untouched", wire[0].Text)
	}
	last := wire[len(wire)-1]
	if !strings.HasPrefix(last.Text, "q4\n\n[Earlier assistant answers 1 and 3") {
		t.Fatalf("last user message = %q, want q4 then the note", last.Text)
	}
	for _, m := range wire[:len(wire)-1] {
		if strings.Contains(m.Text, "different model") {
			t.Fatalf("note leaked into %+v", m)
		}
	}

	image := append(switchHistory()[:7], Message{Role: "user", ContentParts: []MessageContentPart{
		{Type: "text", Text: "q4"},
		{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,iVBORw0KGgo="}},
	}})
	wire = withSwitchNote(toWireMessages(image), image, "deep")
	parts := wire[len(wire)-1].Parts
	if len(parts) != 3 || parts[2].Kind != llmwire.PartText || !strings.Contains(parts[2].Text, "different model") {
		t.Fatalf("picture turn parts = %+v, want the note as a trailing text part", parts)
	}
}

// The request routed to another model carries the note; a deep model that is
// the chat model itself sees no switch.
func TestClient_SwitchedTurnTellsTheModel(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	client := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second,
		Models: Roles{Chat: testModel, Deep: llmwiretest.MessagesModel}}, nil)
	history := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1", Model: testModel},
		{Role: "user", Content: "q2"},
	}
	deep := WithInferenceMetadata(context.Background(), InferenceMetadata{Deep: true})
	if _, err := client.StreamChatWithTools(deep, history, nil, nil); err != nil {
		t.Fatal(err)
	}
	if body := requestBody(t, srv.Last()); !strings.Contains(body, "answer 1 in this thread") {
		t.Fatalf("deep turn after a chat answer: request %s carries no switch note", body)
	}
	if _, err := client.StreamChatWithTools(context.Background(), history, nil, nil); err != nil {
		t.Fatal(err)
	}
	if body := requestBody(t, srv.Last()); strings.Contains(body, "different model") {
		t.Fatalf("same model: request %s carries a switch note", body)
	}

	same := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second,
		Models: Roles{Chat: testModel, Deep: testModel}}, nil)
	if _, err := same.StreamChatWithTools(deep, history, nil, nil); err != nil {
		t.Fatal(err)
	}
	if body := requestBody(t, srv.Last()); strings.Contains(body, "different model") {
		t.Fatalf("deep model is the chat model: request %s carries a switch note", body)
	}
}

// A stopped stream still names its model: the partial answer is kept, and a
// later turn on another model must not take it for its own.
func TestClient_PartialResultNamesItsModel(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	srv.SetReply("a long answer")
	client := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second}, nil)
	stop := errors.New("client gone")
	res, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil,
		func(ev StreamEvent) error {
			if ev.Delta != "" {
				return stop
			}
			return nil
		})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the consumer's", err)
	}
	if res.Model != testModel {
		t.Fatalf("partial model = %q, want %q", res.Model, testModel)
	}
}

func requestBody(t *testing.T, req llmwiretest.Request) string {
	t.Helper()
	raw, err := json.Marshal(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
