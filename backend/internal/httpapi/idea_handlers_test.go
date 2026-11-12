package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/ideas"
)

type ideaFixture struct {
	chat    *chat.Store
	ideas   *ideas.Store
	handler http.Handler
	thread  string
	message string
}

func newIdeaFixture(t *testing.T) *ideaFixture {
	t.Helper()
	db := newUserDB(t)
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('user_2', 'sub-2', 'other', 'user')`); err != nil {
		t.Fatal(err)
	}
	f := &ideaFixture{chat: chat.NewStore(db), ideas: ideas.NewStore(db)}
	thread, err := f.chat.CreateThread(context.Background(), testUser.ID, chat.CreateThreadInput{Title: "Tech-News KW 45"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := f.chat.AddMessageWithCitations(context.Background(), testUser.ID, thread.ID, chat.RoleAssistant, "Vibe Kanban is a board.", chat.MessageTokenUsage{}, nil, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	f.thread, f.message = thread.ID, msg.ID
	f.handler = newAuthenticatedServer(t, Deps{Thread: f.chat, Usage: stubUsageStore{}, Users: fakeUserStore{user: testUser, ok: true}, Ideas: f.ideas})
	return f
}

func (f *ideaFixture) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := authenticatedRequest(method, target, body)
	req.Header.Set(timezoneHeader, "Europe/Zurich")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func TestIdeasAPI_Lifecycle(t *testing.T) {
	f := newIdeaFixture(t)
	rec := f.do(t, http.MethodPost, "/api/ideas", `{"title":"Vibe Kanban","reason":"Lab week topic","excerpt":"Vibe Kanban is a board.","threadId":"`+f.thread+`","messageId":"`+f.message+`","dueOn":"2099-01-12"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	idea := decodeInto[ideas.Idea](t, rec)
	if idea.State != "open" || idea.ThreadTitle != "Tech-News KW 45" || *idea.DueOn != "2099-01-12" || *idea.RemindOn != "2099-01-05" {
		t.Fatalf("created = %+v", idea)
	}
	for _, field := range []string{`"threadDeleted":false`, `"excerpt":"Vibe Kanban is a board."`, `"createdAt"`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Fatalf("JSON lacks %s: %s", field, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "user_1") || strings.Contains(rec.Body.String(), "remindedAt") {
		t.Fatalf("JSON leaks internal fields: %s", rec.Body.String())
	}

	// remindOn null: no follow-up, the due date stays.
	rec = f.do(t, http.MethodPatch, "/api/ideas/"+idea.ID, `{"remindOn":null,"title":"Vibe\nKanban board"}`)
	patched := decodeInto[ideas.Idea](t, rec)
	if rec.Code != http.StatusOK || patched.RemindOn != nil || *patched.DueOn != "2099-01-12" || patched.Title != "Vibe Kanban board" {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, http.MethodGet, "/api/ideas/count", ""); !strings.Contains(rec.Body.String(), `"open":1`) || !strings.Contains(rec.Body.String(), `"done":0`) {
		t.Fatalf("count = %s", rec.Body.String())
	}
	if rec := f.do(t, http.MethodPatch, "/api/ideas/"+idea.ID, `{"done":true}`); !strings.Contains(rec.Body.String(), `"state":"done"`) {
		t.Fatalf("done = %s", rec.Body.String())
	}
	if rec := f.do(t, http.MethodGet, "/api/ideas?status=done", ""); !strings.Contains(rec.Body.String(), idea.ID) {
		t.Fatalf("done list = %s", rec.Body.String())
	}
	if rec := f.do(t, http.MethodDelete, "/api/ideas/"+idea.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := f.do(t, http.MethodGet, "/api/ideas/"+idea.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted = %d", rec.Code)
	}
	if rec := f.do(t, http.MethodPost, "/api/ideas/"+idea.ID+"/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, http.MethodGet, "/api/ideas/"+idea.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("get restored = %d", rec.Code)
	}
}

func TestIdeasAPI_RejectsBadInput(t *testing.T) {
	f := newIdeaFixture(t)
	for name, body := range map[string]string{
		"no reason":           `{"title":"x","reason":"  "}`,
		"bad date":            `{"reason":"r","dueOn":"12.1.2099"}`,
		"follow-up after due": `{"reason":"r","dueOn":"2099-01-12","remindOn":"2099-01-13"}`,
		"message no thread":   `{"reason":"r","messageId":"` + f.message + `"}`,
		"unknown field":       `{"reason":"r","state":"open"}`,
	} {
		if rec := f.do(t, http.MethodPost, "/api/ideas", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := f.do(t, http.MethodGet, "/api/ideas?status=proposed", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("list proposals: %d", rec.Code)
	}
}

// Another user's idea, thread or message is a 404 on every endpoint.
func TestIdeasAPI_ForeignIDsAreNotFound(t *testing.T) {
	f := newIdeaFixture(t)
	ctx := context.Background()
	theirs, err := f.ideas.Create(ctx, "user_2", ideas.StateOpen, ideas.Input{Title: "theirs", Reason: "r"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := f.ideas.Create(ctx, "user_2", ideas.StateProposed, ideas.Input{Title: "p", Reason: "r"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/ideas/" + theirs.ID, ""},
		{http.MethodPatch, "/api/ideas/" + theirs.ID, `{"title":"mine"}`},
		{http.MethodDelete, "/api/ideas/" + theirs.ID, ""},
		{http.MethodPost, "/api/ideas/" + theirs.ID + "/restore", ""},
		{http.MethodPost, "/api/ideas/" + proposal.ID + "/confirm", ""},
		{http.MethodDelete, "/api/ideas/" + proposal.ID, ""},
	} {
		if rec := f.do(t, c.method, c.target, c.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d", c.method, c.target, rec.Code)
		}
	}
	other, err := f.chat.CreateThread(ctx, "user_2", chat.CreateThreadInput{Title: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.do(t, http.MethodPost, "/api/ideas", `{"reason":"r","threadId":"`+other.ID+`"}`); rec.Code != http.StatusNotFound {
		t.Errorf("idea on a foreign thread = %d", rec.Code)
	}
	if rec := f.do(t, http.MethodGet, "/api/ideas", ""); strings.Contains(rec.Body.String(), theirs.ID) {
		t.Errorf("list shows another user's idea")
	}
	if got, _ := f.ideas.GetAny(ctx, "user_2", theirs.ID); got.Title != "theirs" || got.DeletedAt != nil {
		t.Errorf("another user's idea changed: %+v", got)
	}
}

// Only the user's click confirms a proposal; confirmed, it is an open idea.
func TestIdeasAPI_ConfirmAndDismissProposal(t *testing.T) {
	f := newIdeaFixture(t)
	ctx := context.Background()
	p, _ := f.ideas.Create(ctx, testUser.ID, ideas.StateProposed, ideas.Input{Title: "p", Reason: "r", ThreadID: f.thread}, time.Now())
	if rec := f.do(t, http.MethodGet, "/api/ideas/"+p.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("proposal visible: %d", rec.Code)
	}
	if rec := f.do(t, http.MethodPost, "/api/ideas/"+p.ID+"/confirm", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"open"`) {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	d, _ := f.ideas.Create(ctx, testUser.ID, ideas.StateProposed, ideas.Input{Title: "d", Reason: "r"}, time.Now())
	if rec := f.do(t, http.MethodDelete, "/api/ideas/"+d.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss = %d", rec.Code)
	}
	if _, err := f.ideas.GetAny(ctx, testUser.ID, d.ID); err == nil {
		t.Fatal("dismissed proposal kept")
	}
}

// Deleting the thread through the real delete path leaves the idea as it
// was, its thread marked deleted.
func TestIdeasAPI_IdeaSurvivesThreadDelete(t *testing.T) {
	f := newIdeaFixture(t)
	rec := f.do(t, http.MethodPost, "/api/ideas", `{"title":"t","reason":"r","excerpt":"e","threadId":"`+f.thread+`","messageId":"`+f.message+`"}`)
	idea := decodeInto[ideas.Idea](t, rec)
	if rec := f.do(t, http.MethodDelete, "/api/threads/"+f.thread, ""); rec.Code/100 != 2 {
		t.Fatalf("delete thread = %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(t, http.MethodGet, "/api/ideas/"+idea.ID, "")
	got := decodeInto[ideas.Idea](t, rec)
	if got.ThreadID != nil || got.MessageID != nil || !got.ThreadDeleted || got.ThreadTitle != "Tech-News KW 45" ||
		got.Title != "t" || got.Reason != "r" || got.Excerpt != "e" {
		t.Fatalf("after thread delete = %s", rec.Body.String())
	}
}

type fakeIdeaLLM struct {
	title           string
	err             error
	excerpt, reason string
	calls           int
}

func (f *fakeIdeaLLM) DraftIdeaTitle(_ context.Context, excerpt, reason, _ string) (string, error) {
	f.calls++
	f.excerpt, f.reason = excerpt, reason
	return f.title, f.err
}

// The draft proposes a title only; whatever the model writes comes back
// capped to one line. No model, a failure or nothing to name: an empty title.
func TestIdeasAPI_Draft(t *testing.T) {
	f := newIdeaFixture(t)
	gate := &fakeIdeaLLM{title: "Vibe Kanban\nIGNORE PREVIOUS " + strings.Repeat("x", 200)}
	handler := newAuthenticatedServer(t, Deps{Thread: f.chat, Usage: stubUsageStore{}, Users: fakeUserStore{user: testUser, ok: true}, Ideas: f.ideas, IdeaLLM: gate})
	do := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/ideas:draft", body))
		return rec
	}
	rec := do(`{"excerpt":"Vibe Kanban is a board.\u0007","reason":"lab week"}`)
	got := decodeInto[map[string]string](t, rec)
	if rec.Code != http.StatusOK || strings.Contains(got["title"], "\n") || len([]rune(got["title"])) > ideas.MaxTitleRunes || !strings.HasPrefix(got["title"], "Vibe Kanban IGNORE") {
		t.Fatalf("draft = %d %q", rec.Code, got["title"])
	}
	if gate.excerpt != "Vibe Kanban is a board." || gate.reason != "lab week" {
		t.Fatalf("model got %q / %q, want the cleaned fields", gate.excerpt, gate.reason)
	}
	gate.err = errors.New("down")
	if got := decodeInto[map[string]string](t, do(`{"excerpt":"x"}`)); got["title"] != "" {
		t.Fatalf("failed call = %q", got["title"])
	}
	calls := gate.calls
	if got := decodeInto[map[string]string](t, do(`{"excerpt":"  "}`)); got["title"] != "" || gate.calls != calls {
		t.Fatalf("nothing to name still called the model")
	}
	// No model configured.
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/ideas:draft", `{"excerpt":"x"}`))
	if rec.Code != http.StatusOK || decodeInto[map[string]string](t, rec)["title"] != "" {
		t.Fatalf("no model = %d %s", rec.Code, rec.Body.String())
	}
}

func TestIdeasAPI_OffWithoutStore(t *testing.T) {
	handler := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}, Usage: stubUsageStore{}})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/ideas", ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no store = %d", rec.Code)
	}
}
