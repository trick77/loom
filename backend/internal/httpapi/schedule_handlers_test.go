package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/schedule"
)

// fakeScheduleLLM reads every interval as template and reports findings.
type fakeScheduleLLM struct {
	mu       sync.Mutex
	template string
	ok       bool
	findings []llm.Finding
	known    [][]string
}

func (f *fakeScheduleLLM) ParseSchedule(context.Context, string) (string, bool, error) {
	return f.template, f.ok, nil
}

func (f *fakeScheduleLLM) ExtractFindings(_ context.Context, _, _ string, known []string) ([]llm.Finding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known = append(f.known, known)
	return f.findings, nil
}

type scheduleFixture struct {
	db       *sql.DB
	chat     *chat.Store
	store    *schedule.Store
	sllm     *fakeScheduleLLM
	handler  http.Handler
	deps     Deps
	otherUID string
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	db := newUserDB(t)
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('user_2', 'sub-2', 'other', 'user')`); err != nil {
		t.Fatal(err)
	}
	f := &scheduleFixture{
		db:       db,
		chat:     chat.NewStore(db),
		store:    schedule.NewStore(db),
		sllm:     &fakeScheduleLLM{template: "~ 7~22 * * ~", ok: true},
		otherUID: "user_2",
	}
	f.deps = Deps{
		Thread:      f.chat,
		Usage:       stubUsageStore{},
		LLM:         &fakeChatClient{Title: "Cinema scout"},
		Users:       fakeUserStore{user: testUser, ok: true},
		Schedules:   f.store,
		ScheduleLLM: f.sllm,
	}
	f.handler = newAuthenticatedServer(t, f.deps)
	return f
}

func (f *scheduleFixture) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := authenticatedRequest(method, target, body)
	req.Header.Set(timezoneHeader, "Europe/Zurich")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func decodeInto[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestScheduleEndpoints(t *testing.T) {
	f := newScheduleFixture(t)

	rec := f.do(t, http.MethodPost, "/api/schedules:parse", `{"interval":"once a week"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("parse = %d %s", rec.Code, rec.Body)
	}
	preview := decodeInto[schedule.Preview](t, rec)
	if preview.Timezone != "Europe/Zurich" || preview.Template != "~ 7~22 * * ~" || len(preview.NextRuns) != 3 {
		t.Fatalf("preview = %+v", preview)
	}

	body := `{"prompt":"find films","interval":"once a week","template":"` + preview.Template + `","cron":"` + preview.Cron + `"}`
	rec = f.do(t, http.MethodPost, "/api/schedules", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	created := decodeInto[scheduleResponse](t, rec)
	if created.Name != "Cinema scout" || created.Cron != preview.Cron || created.Timezone != "Europe/Zurich" || created.Summary == "" || len(created.NextRuns) != 3 {
		t.Fatalf("created = %+v", created)
	}

	rec = f.do(t, http.MethodGet, "/api/schedules", "")
	list := decodeInto[[]scheduleResponse](t, rec)
	if rec.Code != http.StatusOK || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %d %+v", rec.Code, list)
	}

	rec = f.do(t, http.MethodPatch, "/api/schedules/"+created.ID, `{"name":"Renamed","enabled":false}`)
	updated := decodeInto[scheduleResponse](t, rec)
	if rec.Code != http.StatusOK || updated.Name != "Renamed" || updated.Enabled || len(updated.NextRuns) != 0 || updated.Timezone != "Europe/Zurich" {
		t.Fatalf("update = %d %+v", rec.Code, updated)
	}

	rec = f.do(t, http.MethodGet, "/api/schedules/"+created.ID, "")
	if got := decodeInto[scheduleResponse](t, rec); rec.Code != http.StatusOK || got.Name != "Renamed" {
		t.Fatalf("get = %d %+v", rec.Code, got)
	}
	if rec = f.do(t, http.MethodGet, "/api/schedules/"+created.ID+"/runs", ""); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("runs = %d %s", rec.Code, rec.Body)
	}
	if rec = f.do(t, http.MethodGet, "/api/schedules/"+created.ID+"/memory", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"findings":[]`) {
		t.Fatalf("memory = %d %s", rec.Code, rec.Body)
	}
	for _, kind := range []string{"findings", "sources", "queries"} {
		if rec = f.do(t, http.MethodDelete, "/api/schedules/"+created.ID+"/memory/"+kind+"?key=x", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("forget %s = %d", kind, rec.Code)
		}
	}
	if rec = f.do(t, http.MethodDelete, "/api/schedules/"+created.ID+"/memory/other", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("forget unknown kind = %d", rec.Code)
	}
	if rec = f.do(t, http.MethodDelete, "/api/schedules/"+created.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec = f.do(t, http.MethodGet, "/api/schedules/"+created.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d", rec.Code)
	}
}

func TestScheduleEndpointErrors(t *testing.T) {
	f := newScheduleFixture(t)
	f.sllm.template = "*/5 * * * *"
	if rec := f.do(t, http.MethodPost, "/api/schedules:parse", `{"interval":"every 5 minutes"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "too_often") {
		t.Fatalf("too often = %d %s", rec.Code, rec.Body)
	}
	f.sllm.ok = false
	if rec := f.do(t, http.MethodPost, "/api/schedules:parse", `{"interval":"when it rains"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not_understood") {
		t.Fatalf("not understood = %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, http.MethodPost, "/api/schedules", `{"interval":"daily"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing prompt = %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, http.MethodPost, "/api/schedules", `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	// Another user's schedule is not found.
	other, _ := f.store.CreateSchedule(context.Background(), schedule.Schedule{UserID: f.otherUID, Name: "x", Prompt: "p", IntervalText: "i", CronTemplate: "0 9 * * *", Cron: "0 9 * * *", Timezone: "UTC", Enabled: true})
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/api/schedules/" + other.ID},
		{http.MethodPatch, "/api/schedules/" + other.ID},
		{http.MethodDelete, "/api/schedules/" + other.ID},
		{http.MethodPost, "/api/schedules/" + other.ID + "/run"},
		{http.MethodGet, "/api/schedules/" + other.ID + "/runs"},
		{http.MethodGet, "/api/schedules/" + other.ID + "/memory"},
		{http.MethodDelete, "/api/schedules/" + other.ID + "/memory/findings"},
	} {
		if rec := f.do(t, req.method, req.path, `{"name":"y"}`); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", req.method, req.path, rec.Code)
		}
	}
	// The limit.
	f.sllm.template, f.sllm.ok = "0 9 * * *", true
	for i := 0; i < schedule.MaxSchedulesPerUser; i++ {
		f.do(t, http.MethodPost, "/api/schedules", `{"prompt":"p","interval":"daily","name":"n"}`)
	}
	if rec := f.do(t, http.MethodPost, "/api/schedules", `{"prompt":"p","interval":"daily","name":"n"}`); rec.Code != http.StatusConflict {
		t.Fatalf("over the limit = %d %s", rec.Code, rec.Body)
	}
}

func TestScheduleEndpointsOffWithoutStore(t *testing.T) {
	handler := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}, Usage: stubUsageStore{}})
	for _, path := range []string{"/api/schedules", "/api/notifications"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authenticatedRequest(http.MethodGet, path, ""))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d, want 503", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/notifications/unread", ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("unread without store = %d %s", rec.Code, rec.Body)
	}
}

func TestNotificationEndpoints(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	for _, title := range []string{"a", "b"} {
		_ = f.store.CreateNotification(ctx, testUser.ID, schedule.Notification{Kind: schedule.KindScheduleRun, Title: title})
	}
	rec := f.do(t, http.MethodGet, "/api/notifications/unread", "")
	if !strings.Contains(rec.Body.String(), `"count":2`) {
		t.Fatalf("unread = %s", rec.Body)
	}
	rec = f.do(t, http.MethodGet, "/api/notifications?limit=1", "")
	list := decodeInto[[]schedule.Notification](t, rec)
	if len(list) != 1 || list[0].Title != "b" {
		t.Fatalf("list = %+v", list)
	}
	if rec = f.do(t, http.MethodPost, "/api/notifications/"+list[0].ID+"/read", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("read = %d", rec.Code)
	}
	if rec = f.do(t, http.MethodGet, "/api/notifications/unread", ""); !strings.Contains(rec.Body.String(), `"count":1`) {
		t.Fatalf("unread after read = %s", rec.Body)
	}
	if rec = f.do(t, http.MethodPost, "/api/notifications/read-all", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("read all = %d", rec.Code)
	}
	if rec = f.do(t, http.MethodGet, "/api/notifications/unread", ""); !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("unread after read all = %s", rec.Body)
	}

	// Opening a thread reads the news that points at it, and only that.
	news, _ := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "News"})
	other, _ := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "Other"})
	for _, id := range []string{news.ID, news.ID, other.ID} {
		_ = f.store.CreateNotification(ctx, testUser.ID, schedule.Notification{Kind: schedule.KindScheduleRun, ThreadID: &id, Title: "n"})
	}
	if got, _, _ := f.chat.GetThread(ctx, testUser.ID, news.ID); !got.ScheduleUnread {
		t.Fatal("a thread with unread news is not marked unread")
	}
	if rec = f.do(t, http.MethodPost, "/api/threads/"+news.ID+"/notifications/read", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("read thread = %d", rec.Code)
	}
	if rec = f.do(t, http.MethodGet, "/api/notifications/unread", ""); !strings.Contains(rec.Body.String(), `"count":1`) {
		t.Fatalf("unread after reading one thread = %s", rec.Body)
	}
	if got, _, _ := f.chat.GetThread(ctx, testUser.ID, news.ID); got.ScheduleUnread {
		t.Fatal("a read thread is still marked unread")
	}
	if got, _, _ := f.chat.GetThread(ctx, testUser.ID, other.ID); !got.ScheduleUnread {
		t.Fatal("reading one thread read another's news")
	}
}

func TestRunThreadTakesAFollowUp(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	sch, _ := f.store.CreateSchedule(ctx, schedule.Schedule{UserID: testUser.ID, Name: "s", Prompt: "p", IntervalText: "i", CronTemplate: "0 9 * * *", Cron: "0 9 * * *", Timezone: "UTC", Enabled: true})
	thread, _ := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "Cinema scout", ScheduleID: &sch.ID})
	runID, _ := f.store.StartRun(ctx, testUser.ID, sch.ID, time.Now())
	_ = f.store.AttachRunThread(ctx, testUser.ID, runID, thread.ID)
	// While the run still writes the thread, nothing joins it.
	for _, path := range []string{"messages:stream", "messages:queue"} {
		rec := f.do(t, http.MethodPost, "/api/threads/"+thread.ID+"/"+path, `{"content":"too early","clientMessageId":"c0"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s during the run = %d %s", path, rec.Code, rec.Body)
		}
	}
	_ = f.store.FinishRun(ctx, testUser.ID, schedule.Run{ID: runID, Status: schedule.StatusNothingNew}, time.Now())
	rec := f.do(t, http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"tell me more","clientMessageId":"c1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up in a run thread = %d %s", rec.Code, rec.Body)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, thread.ID)
	if len(messages) == 0 || messages[0].Content != "tell me more" {
		t.Fatalf("thread = %+v", messages)
	}
}

func TestScheduleRunsAdmission(t *testing.T) {
	r := newScheduleRuns()
	if !r.admit("a") || r.admit("a") {
		t.Fatal("one run per user")
	}
	if !r.admit("b") || r.admit("c") {
		t.Fatal("at most two runs at once")
	}
	r.done("a")
	if !r.admit("c") {
		t.Fatal("a finished run frees its slot")
	}
}

func TestRememberTimezone(t *testing.T) {
	s := &server{}
	for _, tz := range []string{"", "Mars/Olympus", strings.Repeat("x", 65)} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(timezoneHeader, tz)
		s.rememberTimezone("u", req)
	}
	if got := s.timezoneFor("u"); got != "" {
		t.Fatalf("a bad timezone was kept: %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(timezoneHeader, "Europe/Zurich")
	s.rememberTimezone("u", req)
	if got := s.timezoneFor("u"); got != "Europe/Zurich" {
		t.Fatalf("timezone = %q", got)
	}
}

func TestWriteScheduleErrorFallsBackTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeScheduleError(rec, httptest.NewRequest(http.MethodGet, "/", nil), errors.New("db down"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
}
