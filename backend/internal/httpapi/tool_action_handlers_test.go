package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/toolactions"
	"github.com/trick77/loom/internal/turn"
)

type actionFixture struct {
	chat    *chat.Store
	actions *toolactions.Store
	handler http.Handler
	thread  string
	calls   *[]map[string]any
}

const addTool = "peeq__add_videos"

func newActionFixture(t *testing.T, mutate func(*fakeMCPService)) *actionFixture {
	t.Helper()
	db := newUserDB(t)
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('user_2', 'sub-2', 'other', 'user')`); err != nil {
		t.Fatal(err)
	}
	f := &actionFixture{chat: chat.NewStore(db), actions: toolactions.NewStore(db), calls: &[]map[string]any{}}
	thread, err := f.chat.CreateThread(context.Background(), testUser.ID, chat.CreateThreadInput{Title: "Videos"})
	if err != nil {
		t.Fatal(err)
	}
	f.thread = thread.ID
	tools := fakeMCPService{
		Available: map[string]bool{addTool: true},
		Acting:    map[string]bool{addTool: true},
		Confirm:   map[string]bool{addTool: true},
		Titles:    map[string]string{addTool: "Add videos to your library"},
		Items:     []mcp.ActionItem{{Status: mcp.ActionDone, Message: "queued"}},
		Link:      &mcp.ActionLink{URL: "https://peeq.example/activity", Label: "peeq"},
		CallFunc: func(ctx context.Context, _ string, args map[string]any) (string, error) {
			if p, ok := mcp.PrincipalFrom(ctx); !ok || p.ID != testUser.ID {
				t.Errorf("call made without the user: %+v", p)
			}
			*f.calls = append(*f.calls, args)
			return "queued: https://youtu.be/Zr7KcW1nB0e", nil
		},
	}
	if mutate != nil {
		mutate(&tools)
	}
	f.handler = newAuthenticatedServer(t, Deps{Thread: f.chat, Usage: stubUsageStore{}, Users: fakeUserStore{user: testUser, ok: true}, MCP: tools, ToolActions: f.actions})
	return f
}

func (f *actionFixture) propose(t *testing.T, userID string) toolactions.Action {
	t.Helper()
	thread := f.thread
	if userID != testUser.ID {
		other, err := f.chat.CreateThread(context.Background(), userID, chat.CreateThreadInput{Title: "Bob"})
		if err != nil {
			t.Fatal(err)
		}
		thread = other.ID
	}
	a, err := f.actions.Propose(context.Background(), userID, thread, addTool, json.RawMessage(`{"videos":[{"url":"https://youtu.be/Zr7KcW1nB0e"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *actionFixture) post(t *testing.T, target string) (*httptest.ResponseRecorder, turn.ActionCard) {
	t.Helper()
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, authenticatedRequest(http.MethodPost, target, ""))
	var card turn.ActionCard
	_ = json.Unmarshal(rec.Body.Bytes(), &card)
	return rec, card
}

// The click runs the call once, as the user, with the stored arguments, and
// the card settles with the server's result. A second click runs nothing.
func TestToolActionRun(t *testing.T) {
	f := newActionFixture(t, nil)
	a := f.propose(t, testUser.ID)
	rec, card := f.post(t, "/api/tool-actions/"+a.ID+"/run")
	if rec.Code != http.StatusOK || card.State != toolactions.StateDone || card.Title != "Add videos to your library" || card.Server != "peeq" {
		t.Fatalf("run = %d %s", rec.Code, rec.Body)
	}
	if card.Result == nil || card.Result.Items[0].Message != "queued" || card.Result.Link.Label != "peeq" || !strings.HasPrefix(card.Result.Text, "queued:") {
		t.Fatalf("result = %+v", card.Result)
	}
	if len(*f.calls) != 1 {
		t.Fatalf("calls = %d", len(*f.calls))
	}
	videos, _ := (*f.calls)[0]["videos"].([]any)
	if len(videos) != 1 || videos[0].(map[string]any)["url"] != "https://youtu.be/Zr7KcW1nB0e" {
		t.Fatalf("args = %+v", (*f.calls)[0])
	}
	rec, card = f.post(t, "/api/tool-actions/"+a.ID+"/run")
	if rec.Code != http.StatusConflict || card.State != toolactions.StateDone || len(*f.calls) != 1 {
		t.Fatalf("second run = %d %s, calls %d", rec.Code, rec.Body, len(*f.calls))
	}
}

// Another user's action is not found, and nothing runs.
func TestToolActionForeignIsNotFound(t *testing.T) {
	f := newActionFixture(t, nil)
	a := f.propose(t, "user_2")
	for _, path := range []string{"/run", "/dismiss"} {
		if rec, _ := f.post(t, "/api/tool-actions/"+a.ID+path); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d", path, rec.Code)
		}
	}
	if len(*f.calls) != 0 {
		t.Fatalf("a foreign action ran")
	}
}

// Dismissed, it can no longer run.
func TestToolActionDismiss(t *testing.T) {
	f := newActionFixture(t, nil)
	a := f.propose(t, testUser.ID)
	if rec, card := f.post(t, "/api/tool-actions/"+a.ID+"/dismiss"); rec.Code != http.StatusOK || card.State != toolactions.StateDismissed {
		t.Fatalf("dismiss = %d %s", rec.Code, rec.Body)
	}
	if rec, _ := f.post(t, "/api/tool-actions/"+a.ID+"/run"); rec.Code != http.StatusConflict || len(*f.calls) != 0 {
		t.Fatalf("run after dismiss = %d, calls %d", rec.Code, len(*f.calls))
	}
}

// A tool the user may no longer use fails without a call; the server's own
// error text is the card's, any other failure says nothing of loom's
// internals.
func TestToolActionFailures(t *testing.T) {
	f := newActionFixture(t, func(m *fakeMCPService) { m.Users = map[string][]string{"peeq": {"someone-else"}} })
	a := f.propose(t, testUser.ID)
	if _, card := f.post(t, "/api/tool-actions/"+a.ID+"/run"); card.State != toolactions.StateFailed || len(*f.calls) != 0 {
		t.Fatalf("denied tool: %+v, calls %d", card, len(*f.calls))
	}
	f = newActionFixture(t, func(m *fakeMCPService) {
		m.CallFunc = func(context.Context, string, map[string]any) (string, error) {
			return "", &mcp.ToolError{Tool: addTool, Text: "at most 10 videos per call"}
		}
	})
	a = f.propose(t, testUser.ID)
	if _, card := f.post(t, "/api/tool-actions/"+a.ID+"/run"); card.State != toolactions.StateFailed || card.Result == nil || card.Result.Text != "at most 10 videos per call" {
		t.Fatalf("tool error: %+v", card)
	}
	f = newActionFixture(t, func(m *fakeMCPService) { m.Err = context.DeadlineExceeded; m.CallFunc = nil })
	a = f.propose(t, testUser.ID)
	if _, card := f.post(t, "/api/tool-actions/"+a.ID+"/run"); card.State != toolactions.StateFailed || (card.Result != nil && card.Result.Text != "") {
		t.Fatalf("transport error: %+v", card)
	}
}

// GET is the card as it stands; another user's is not found. A claim that
// never settled reads as unknown, never as failed: the call may have run.
func TestToolActionGetAndStaleRunning(t *testing.T) {
	f := newActionFixture(t, nil)
	a := f.propose(t, testUser.ID)
	get := func(id string) (*httptest.ResponseRecorder, turn.ActionCard) {
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/tool-actions/"+id, ""))
		var card turn.ActionCard
		_ = json.Unmarshal(rec.Body.Bytes(), &card)
		return rec, card
	}
	if rec, card := get(a.ID); rec.Code != http.StatusOK || card.State != toolactions.StateProposed || card.Title != "Add videos to your library" {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if rec, _ := get(f.propose(t, "user_2").ID); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign get = %d", rec.Code)
	}
	if _, err := f.actions.Claim(context.Background(), testUser.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, card := get(a.ID); card.State != toolactions.StateRunning {
		t.Fatalf("fresh claim = %s", card.State)
	}
	stale := time.Now().Add(-staleRunning - time.Minute)
	card := withAction(turn.ActionCard{}, toolactions.Action{ID: a.ID, State: toolactions.StateRunning, CreatedAt: stale, DecidedAt: &stale})
	if card.State != turn.ActionCardUnknown {
		t.Fatalf("stale claim = %s", card.State)
	}
}

// A thread's stored card shows what became of its action on every load.
func TestToolActionOverlay(t *testing.T) {
	f := newActionFixture(t, nil)
	a := f.propose(t, testUser.ID)
	blocks, _ := json.Marshal([]turn.ContentBlock{
		{Type: "text", Content: "Here."},
		{Type: "action", Action: &turn.ActionCard{ActionID: a.ID, State: toolactions.StateProposed, Server: "peeq", Title: "Add videos to your library", Arguments: a.Arguments}},
		{Type: "action", Action: &turn.ActionCard{ActionID: "missing", State: toolactions.StateProposed}},
	})
	if _, err := f.chat.AddMessageWithCitations(context.Background(), testUser.ID, f.thread, chat.RoleAssistant, "Here.", chat.MessageTokenUsage{}, nil, nil, nil, blocks, false); err != nil {
		t.Fatal(err)
	}
	f.post(t, "/api/tool-actions/"+a.ID+"/run")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/"+f.thread, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("messages = %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"state":"done"`) || !strings.Contains(body, `"message":"queued"`) || !strings.Contains(body, `"state":"gone"`) {
		t.Fatalf("overlay missing:\n%s", body)
	}
}
