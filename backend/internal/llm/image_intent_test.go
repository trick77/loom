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

// intentServer stands up a fake chat-completions endpoint that captures the
// request body and replies with the given assistant content.
func intentServer(t *testing.T, reply string, captured *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if captured != nil {
			*captured = string(body)
		}
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": reply},
				"finish_reason": "stop",
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func TestClassifyImageIntent_parsesReplyAndForwardsFlags(t *testing.T) {
	var body string
	srv := intentServer(t, `{"action":"create","needs_text":true}`, &body)
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())

	intent, err := c.ClassifyImageIntent(context.Background(), "erstelle ein Logo fuer eine Baeckerei", false, true)
	if err != nil {
		t.Fatalf("ClassifyImageIntent error = %v", err)
	}
	if intent.Action != ImageIntentCreate || !intent.NeedsText {
		t.Fatalf("intent = %+v, want create + needs_text", intent)
	}
	// The user's own (German) words and both presence flags must reach the model.
	if !strings.Contains(body, "erstelle ein Logo") {
		t.Errorf("request body did not carry the user message: %s", body)
	}
	if !strings.Contains(body, "image_attached_this_turn=false") || !strings.Contains(body, "conversation_already_has_an_image=true") {
		t.Errorf("request body did not carry the presence flags: %s", body)
	}
}

func TestClassifyImageIntent_toleratesCodeFenceAndProse(t *testing.T) {
	srv := intentServer(t, "Sure! ```json\n{\"action\":\"edit\",\"needs_text\":false}\n```", nil)
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())

	intent, err := c.ClassifyImageIntent(context.Background(), "make it bigger", false, true)
	if err != nil {
		t.Fatalf("ClassifyImageIntent error = %v", err)
	}
	if intent.Action != ImageIntentEdit || intent.NeedsText {
		t.Fatalf("intent = %+v, want edit + no needs_text", intent)
	}
}

func TestClassifyImageIntent_failsSafeToNone(t *testing.T) {
	// Unparseable reply -> none, no error.
	srv := intentServer(t, "I cannot decide.", nil)
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if intent, err := c.ClassifyImageIntent(context.Background(), "hi", false, false); err != nil || intent.Action != ImageIntentNone {
		t.Fatalf("garbage reply: intent %+v err %v, want none/nil", intent, err)
	}

	// Transport error -> none, error surfaced for logging.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer errSrv.Close()
	ec := NewClient(Config{BaseURL: errSrv.URL, APIKey: "k"}, errSrv.Client())
	if intent, err := ec.ClassifyImageIntent(context.Background(), "hi", false, false); err == nil || intent.Action != ImageIntentNone {
		t.Fatalf("http error: intent %+v err %v, want none/err", intent, err)
	}
}

func TestParseImageIntent(t *testing.T) {
	tests := []struct {
		reply string
		want  ImageIntent
	}{
		{`{"action":"create","needs_text":false}`, ImageIntent{Action: ImageIntentCreate}},
		{`{"action":"create","needs_text":true}`, ImageIntent{Action: ImageIntentCreate, NeedsText: true}},
		{`{"action":"edit","needs_text":true}`, ImageIntent{Action: ImageIntentEdit, NeedsText: true}},
		{`{"action":"none","needs_text":false}`, ImageIntent{Action: ImageIntentNone}},
		{`{"action":"CREATE"}`, ImageIntent{Action: ImageIntentCreate}}, // case-insensitive
		{`{"action":"nonsense"}`, ImageIntent{Action: ImageIntentNone}}, // unknown -> none
		{`not json at all`, ImageIntent{Action: ImageIntentNone}},
		{``, ImageIntent{Action: ImageIntentNone}},
		// needs_text on a none reply is dropped (meaningless without an image action).
		{`{"action":"none","needs_text":true}`, ImageIntent{Action: ImageIntentNone}},
	}
	for _, tt := range tests {
		if got := parseImageIntent(tt.reply); got != tt.want {
			t.Errorf("parseImageIntent(%q) = %+v, want %+v", tt.reply, got, tt.want)
		}
	}
}
