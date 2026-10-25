package httpapi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/schedule"
)

func fetchCall(id, url string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunction{Name: "fetch__fetch", Arguments: `{"url":"` + url + `"}`}}
}

func webTools() []llm.Tool {
	var tools []llm.Tool
	for _, name := range []string{"fetch__fetch", "tavily__tavily_search", "context7__docs"} {
		tools = append(tools, llm.Tool{Type: "function", Function: llm.ToolFunction{Name: name, Parameters: map[string]any{"type": "object"}}})
	}
	return tools
}

// newRunnerServer is a server with real stores whose model fetches one page
// and answers, rounds times over.
func newRunnerServer(t *testing.T, f *scheduleFixture, client *fakeToolChatClient) *server {
	t.Helper()
	deps := f.deps
	deps.LLM = client
	deps.MCP = &fakeMCPService{ToolList: webTools(), Result: "Dune: Part Three opens 17 Dec."}
	return newServer(deps)
}

func researchRound(answer string) []llm.StreamResult {
	return []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{fetchCall("c1", "https://www.arena.ch/programm")}},
		{Content: answer},
	}
}

func newRunnerSchedule(t *testing.T, f *scheduleFixture) schedule.Schedule {
	t.Helper()
	sch, err := f.store.CreateSchedule(context.Background(), schedule.Schedule{
		UserID: testUser.ID, Name: "Cinema scout", Prompt: "find new films", IntervalText: "once a week",
		CronTemplate: "~ 7~22 * * ~", Cron: "37 14 * * 4", Timezone: "Europe/Zurich", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestScheduledRunReportsOnlyWhatIsNew(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.sllm.findings = []llm.Finding{{Key: "dune-part-three", Label: "Dune: Part Three", Source: 1}}
	client := &fakeToolChatClient{Results: researchRound("New: Dune: Part Three [1].")}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)

	s.runSchedule(ctx, sch)

	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusNew || runs[0].NewCount != 1 || runs[0].MessageID == nil || runs[0].Rounds != 1 {
		t.Fatalf("first run = %+v", runs)
	}
	// Only the research tools were offered: never the schedule tools or
	// unrelated MCP servers.
	var offered []string
	for _, tool := range client.Tools[0] {
		offered = append(offered, tool.Function.Name)
	}
	slices.Sort(offered)
	if strings.Join(offered, ",") != "fetch__fetch,tavily__tavily_search" {
		t.Fatalf("offered tools = %v", offered)
	}
	if !strings.Contains(client.Histories[0][0].Content, "running a scheduled task") {
		t.Fatal("the run's system prompt does not say it runs unattended")
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if got.ThreadID == nil {
		t.Fatal("the run did not create the schedule's thread")
	}
	thread, _, _ := f.chat.GetThread(ctx, testUser.ID, *got.ThreadID)
	if thread.ScheduleID == nil || thread.Title != "Cinema scout" {
		t.Fatalf("thread = %+v", thread)
	}
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 || notes[0].Title != "Cinema scout: 1 new" || notes[0].Body != "Dune: Part Three" || notes[0].MessageID == nil {
		t.Fatalf("notifications = %+v", notes)
	}
	findings, _ := f.store.ListFindings(ctx, testUser.ID, sch.ID)
	if len(findings) != 1 || findings[0].SourceURL != "https://www.arena.ch/programm" {
		t.Fatalf("findings = %+v", findings)
	}
	sources, _ := f.store.ListSources(ctx, testUser.ID, sch.ID)
	if len(sources) != 1 || sources[0].URLKey != "arena.ch/programm" || sources[0].Finds != 1 || sources[0].Runs != 1 {
		t.Fatalf("sources = %+v", sources)
	}
	activityAfterNews := thread.LastMessageAt

	// The second run reports the same film: nothing new, no notification, and
	// the thread stays where it was in the recents.
	time.Sleep(1100 * time.Millisecond)
	client.Results = researchRound("Still Dune: Part Three [1].")
	s.runSchedule(ctx, got)
	runs, _ = f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 2 || runs[0].Status != schedule.StatusNothingNew || runs[0].NewCount != 0 {
		t.Fatalf("second run = %+v", runs[0])
	}
	if notes, _ = f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 1 {
		t.Fatalf("a run without news notified: %+v", notes)
	}
	thread, _, _ = f.chat.GetThread(ctx, testUser.ID, *got.ThreadID)
	if !thread.LastMessageAt.Equal(*activityAfterNews) {
		t.Fatalf("thread activity moved from %v to %v on a run without news", activityAfterNews, thread.LastMessageAt)
	}
	// The second run saw the schedule's memory, and the extractor the known key.
	last := client.Histories[len(client.Histories)-1][0].Content
	if !strings.Contains(last, "Already reported") || !strings.Contains(last, "Dune: Part Three") || !strings.Contains(last, "arena.ch/programm: 1 finds") {
		t.Fatalf("second run's prompt lacks the memory:\n%s", last)
	}
	if known := f.sllm.known[1]; len(known) != 1 || known[0] != "dune-part-three" {
		t.Fatalf("known keys = %v", known)
	}
	// The note is about the previous run, never the one in progress.
	if !strings.Contains(last, "Last run took") || !strings.Contains(last, "1 new items") {
		t.Fatalf("second run's prompt lacks the last-run note:\n%s", last)
	}
}

func TestScheduledRunRefusesToolsOutsideItsSet(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	var called []string
	client := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "context7__docs", Arguments: `{}`}}}},
		{Content: "Nothing new."},
	}}
	deps := f.deps
	deps.LLM = client
	deps.MCP = &fakeMCPService{ToolList: webTools(), CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
		called = append(called, name)
		return "docs", nil
	}}
	s := newServer(deps)
	s.runSchedule(ctx, newRunnerSchedule(t, f))
	if len(called) != 0 {
		t.Fatalf("a tool outside the run's set ran: %v", called)
	}
	last := client.Histories[len(client.Histories)-1]
	if !strings.Contains(last[len(last)-1].Content, "unknown tool") {
		t.Fatalf("the model was not told the tool is unknown: %+v", last[len(last)-1])
	}
}

func TestScheduledRunCutByShutdownLeavesANote(t *testing.T) {
	f := newScheduleFixture(t)
	blocking := &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	f.deps.LLM = blocking
	s := newServer(f.deps)
	sch := newRunnerSchedule(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		s.runSchedule(ctx, sch)
		close(finished)
	}()
	<-blocking.started
	cancel()
	<-finished
	got, _ := f.store.GetSchedule(context.Background(), testUser.ID, sch.ID)
	messages, _, _ := f.chat.ListMessages(context.Background(), testUser.ID, *got.ThreadID)
	if len(messages) != 2 || !strings.Contains(messages[1].Content, "interrupted by a restart") {
		t.Fatalf("thread = %+v", messages)
	}
	if !got.Enabled {
		t.Fatal("a shutdown disabled the schedule")
	}
}

func TestScheduledRunStopsAtTheTokenBudget(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.sllm.findings = []llm.Finding{{Key: "a", Label: "A"}}
	client := &fakeToolChatClient{
		Results: []llm.StreamResult{{ToolCalls: []llm.ToolCall{fetchCall("c1", "https://a.example")}, Usage: llm.TokenUsage{TotalTokens: schedule.RunTokenBudget}}},
		Plain:   "Found A.",
	}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusTokenLimit || runs[0].NewCount != 1 || runs[0].Tokens < schedule.RunTokenBudget {
		t.Fatalf("run = %+v", runs)
	}
	// The forced final answer offered no tools.
	if len(client.Tools[len(client.Tools)-1]) != 0 {
		t.Fatal("the final answer after the budget still offered tools")
	}
}

func TestScheduledRunPastTheTimeLimitDisablesTheSchedule(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.deps.LLM = &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	s := newServer(f.deps)
	s.runTimeout = 50 * time.Millisecond
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch)

	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusTimeout {
		t.Fatalf("run = %+v", runs)
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if got.Enabled || got.DisabledReason != schedule.DisabledTimeout || got.DisabledAt == nil {
		t.Fatalf("schedule after timeout = %+v", got)
	}
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 || notes[0].Kind != schedule.KindScheduleDisabled || !strings.Contains(notes[0].Body, "3 minutes") {
		t.Fatalf("notifications = %+v", notes)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
	if len(messages) != 2 || !strings.Contains(messages[1].Content, "disabled") {
		t.Fatalf("thread = %+v", messages)
	}
}

func TestScheduledRunFailureLeavesANote(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.deps.LLM = &fakeChatClient{StreamErr: errors.New("upstream down")}
	s := newServer(f.deps)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusFailed || runs[0].Error == "" {
		t.Fatalf("run = %+v", runs)
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if !got.Enabled {
		t.Fatal("a failed run disabled the schedule")
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
	if len(messages) != 2 || !strings.HasPrefix(messages[1].Content, "This scheduled run failed") {
		t.Fatalf("thread = %+v", messages)
	}
	if notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 0 {
		t.Fatalf("a failed run notified: %+v", notes)
	}
}

func TestScheduledRunRecreatesADeletedThread(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: researchRound("Nothing new.")}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	gone := "gone"
	sch.ThreadID = &gone
	s.runSchedule(ctx, sch)
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if got.ThreadID == nil || *got.ThreadID == gone {
		t.Fatalf("thread = %v", got.ThreadID)
	}
}

func TestScheduleWorkerStartsDueRuns(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: researchRound("Nothing new.")}
	s := newRunnerServer(t, f, client)
	past := time.Now().Add(-time.Minute)
	sch := newRunnerSchedule(t, f)
	sch.NextRunAt = &past
	if _, err := f.store.SaveSchedule(ctx, sch); err != nil {
		t.Fatal(err)
	}
	(&ScheduleWorker{s: s}).startDue(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
		got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
		if len(runs) == 1 && runs[0].Status != schedule.StatusRunning && !got.Running {
			if got.NextRunAt == nil || !got.NextRunAt.After(time.Now()) {
				t.Fatalf("next run = %v, want a future slot", got.NextRunAt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the due run did not finish: %+v", runs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestScheduleWorkerRunResetsAndStops(t *testing.T) {
	f := newScheduleFixture(t)
	s := newServer(f.deps)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		(&ScheduleWorker{s: s}).Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not stop")
	}
	// Without schedules the worker returns at once.
	(&ScheduleWorker{s: newServer(Deps{Thread: &fakeThreadStore{}})}).Run(context.Background())
}

func TestRunScheduleEndpoint(t *testing.T) {
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: researchRound("Nothing new.")}
	f.deps.LLM = client
	f.deps.MCP = &fakeMCPService{ToolList: webTools(), Result: "page"}
	f.handler = newAuthenticatedServer(t, f.deps)
	sch := newRunnerSchedule(t, f)
	if rec := f.do(t, "POST", "/api/schedules/"+sch.ID+"/run", ""); rec.Code != 202 {
		t.Fatalf("run now = %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, _ := f.store.ListRuns(context.Background(), testUser.ID, sch.ID, 5)
		if len(runs) == 1 && runs[0].Status != schedule.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run now did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestScheduleToolRunner(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newServer(f.deps)
	tools := scheduleToolRunner{s: s}

	if out := tools.List(ctx, testUser); out != "The user has no scheduled tasks." {
		t.Fatalf("empty list = %q", out)
	}
	out := tools.Create(ctx, testUser, "Europe/Zurich", map[string]any{"prompt": "find films", "interval": "once a week", "name": "Cinema scout"})
	if !strings.HasPrefix(out, `Created. "Cinema scout" (id `) || !strings.Contains(out, "Europe/Zurich") || !strings.Contains(out, "Weekly on") {
		t.Fatalf("create = %q", out)
	}
	list, _ := f.store.ListSchedules(ctx, testUser.ID)
	id := list[0].ID
	if out := tools.List(ctx, testUser); !strings.Contains(out, id) {
		t.Fatalf("list = %q", out)
	}
	if out := tools.Update(ctx, testUser, "", map[string]any{"id": id, "enabled": false}); !strings.Contains(out, "paused") {
		t.Fatalf("pause = %q", out)
	}
	if out := tools.Update(ctx, testUser, "", map[string]any{}); !strings.Contains(out, "id is required") {
		t.Fatalf("update without id = %q", out)
	}
	if out := tools.Update(ctx, testUser, "", map[string]any{"id": "nope", "name": "x"}); !strings.Contains(out, "No schedule has that id") {
		t.Fatalf("update unknown = %q", out)
	}
	f.sllm.template = "*/5 * * * *"
	if out := tools.Update(ctx, testUser, "", map[string]any{"id": id, "interval": "every 5 minutes"}); !strings.Contains(out, "at most twice a day") {
		t.Fatalf("too often = %q", out)
	}
	f.sllm.ok = false
	if out := tools.Create(ctx, testUser, "", map[string]any{"prompt": "p", "interval": "when it rains"}); !strings.Contains(out, "names no recurring time") {
		t.Fatalf("not understood = %q", out)
	}
	if out := tools.Create(ctx, testUser, "", map[string]any{"interval": "daily"}); !strings.Contains(out, "Could not save") {
		t.Fatalf("missing prompt = %q", out)
	}
	if out := tools.Delete(ctx, testUser, map[string]any{}); !strings.Contains(out, "id is required") {
		t.Fatalf("delete without id = %q", out)
	}
	if out := tools.Delete(ctx, testUser, map[string]any{"id": id}); !strings.Contains(out, `Deleted the schedule "Cinema scout"`) {
		t.Fatalf("delete = %q", out)
	}
	if out := tools.Delete(ctx, testUser, map[string]any{"id": id}); !strings.Contains(out, "No schedule has that id") {
		t.Fatalf("delete twice = %q", out)
	}
	if got := scheduleToolError(schedule.ErrLimitReached); !strings.Contains(got, "most allowed") {
		t.Fatalf("limit error = %q", got)
	}
	if got := scheduleToolError(errors.New("boom")); got != "tool failed: boom" {
		t.Fatalf("other error = %q", got)
	}
}

func TestScheduleNamerAndTitles(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newServer(f.deps)
	if name, err := (scheduleNamer{s: s}).NameSchedule(ctx, testUser.ID, "find films"); err != nil || name != "Cinema scout" {
		t.Fatalf("name = %q, %v", name, err)
	}
	thread, _ := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "old"})
	if err := (scheduleThreadTitles{s: s}).SetThreadTitle(ctx, testUser.ID, thread.ID, "new"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := f.chat.GetThread(ctx, testUser.ID, thread.ID)
	if got.Title != "new" {
		t.Fatalf("title = %q", got.Title)
	}
}
