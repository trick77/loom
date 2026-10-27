package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/inference"
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

// researchClient is a model that researches one page and answers answer.
func researchClient(answer string) *fakeToolChatClient {
	return &fakeToolChatClient{Results: researchRound(answer), Plain: answer}
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

// lastRunThread is the thread the schedule's latest run started.
func lastRunThread(t *testing.T, f *scheduleFixture, sch schedule.Schedule) string {
	t.Helper()
	runs, err := f.store.ListRuns(context.Background(), testUser.ID, sch.ID, 1)
	if err != nil || len(runs) != 1 || runs[0].ThreadID == nil {
		t.Fatalf("latest run = %+v, %v: no thread", runs, err)
	}
	return *runs[0].ThreadID
}

func TestScheduledRunReportsOnlyWhatIsNew(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.sllm.findings = []llm.Finding{{Key: "dune-part-three", Label: "Dune: Part Three", Source: 1}}
	client := researchClient("New: Dune: Part Three [1].")
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)

	s.runSchedule(ctx, sch, timedRun)

	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusNew || runs[0].NewCount != 1 || runs[0].MessageID == nil || runs[0].Rounds != 1 {
		t.Fatalf("first run = %+v", runs)
	}
	// Every MCP tool the user may use was offered, and no built-in: never the
	// schedule, memory or file tools.
	var offered []string
	for _, tool := range client.Tools[0] {
		offered = append(offered, tool.Function.Name)
	}
	slices.Sort(offered)
	if strings.Join(offered, ",") != "context7__docs,fetch__fetch,tavily__tavily_search" {
		t.Fatalf("offered tools = %v", offered)
	}
	if !strings.Contains(client.Histories[0][0].Content, "running a scheduled task") {
		t.Fatal("the run's system prompt does not say it runs unattended")
	}
	firstThread := lastRunThread(t, f, sch)
	thread, _, _ := f.chat.GetThread(ctx, testUser.ID, firstThread)
	if thread.ScheduleID == nil || *thread.ScheduleID != sch.ID || thread.Title != "Cinema scout" {
		t.Fatalf("thread = %+v", thread)
	}
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 || notes[0].Title != "Cinema scout: 1 new" || notes[0].Body != "Dune: Part Three" || notes[0].MessageID == nil ||
		notes[0].ThreadID == nil || *notes[0].ThreadID != firstThread {
		t.Fatalf("notifications = %+v", notes)
	}
	if strings.Join(runs[0].NewLabels, ",") != "Dune: Part Three" {
		t.Fatalf("run labels = %v", runs[0].NewLabels)
	}
	sources, _ := f.store.ListSources(ctx, testUser.ID, sch.ID)
	if len(sources) != 1 || sources[0].URLKey != "arena.ch/programm" || sources[0].Finds != 1 || sources[0].Runs != 1 {
		t.Fatalf("sources = %+v", sources)
	}

	// The second and third runs find nothing new: no notification and no
	// thread left behind. The first thread stays, its news read or not: it
	// is the latest there is.
	f.sllm.findings = []llm.Finding{}
	client.Results, client.Plain = researchRound("Nothing new since the last run [1]."), "Nothing new since the last run [1]."
	s.runSchedule(ctx, sch, timedRun)
	last := client.Histories[len(client.Histories)-1][0].Content
	if err := f.store.MarkRead(ctx, testUser.ID, notes[0].ID); err != nil {
		t.Fatal(err)
	}
	client.Results = researchRound("Nothing new since the last run [1].")
	s.runSchedule(ctx, sch, timedRun)
	runs, _ = f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 3 || runs[0].ThreadID != nil || runs[1].ThreadID != nil || runs[2].ThreadID == nil || *runs[2].ThreadID != firstThread {
		t.Fatalf("runs = %+v, want only the first still linked to a thread", runs)
	}
	if runs[0].Status != schedule.StatusNothingNew || runs[0].NewCount != 0 || runs[1].Status != schedule.StatusNothingNew {
		t.Fatalf("later runs = %+v", runs[:2])
	}
	if notes, _ = f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 1 {
		t.Fatalf("a run without news notified: %+v", notes)
	}
	if threads, _ := f.chat.ListThreads(ctx, testUser.ID, chat.ListThreadsOptions{}); len(threads) != 1 || threads[0].ID != firstThread {
		t.Fatalf("threads = %+v, want only the first run's", threads)
	}

	// An extraction that cannot tell keeps its run's thread, the answer may
	// hold news, and replaces the first one as any run does.
	f.sllm.findings = nil
	client.Results = researchRound("Maybe something [1].")
	s.runSchedule(ctx, sch, timedRun)
	lastThread := lastRunThread(t, f, sch)
	if lastThread == firstThread {
		t.Fatal("a later run wrote into the first run's thread")
	}
	if messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastThread); len(messages) != 2 || messages[0].Content != "find new films" {
		t.Fatalf("thread %s = %+v, want the run's prompt and answer only", lastThread, messages)
	}
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, firstThread); found {
		t.Fatal("a run kept the first run's thread after its news was read")
	}
	if notes, _ = f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 1 || notes[0].ThreadID != nil {
		t.Fatalf("notifications = %+v, want the first one only, no longer linking its replaced thread", notes)
	}
	// The second run saw the schedule's memory: the learned sources reach
	// every call, nothing a run reported reaches any.
	if !strings.Contains(last, "arena.ch/programm: 1 finds") {
		t.Fatalf("second run's prompt lacks the memory:\n%s", last)
	}
	for i, history := range client.Histories {
		if strings.Contains(history[0].Content, "Dune") {
			t.Fatalf("call %d names an item an earlier run reported:\n%s", i, history[0].Content)
		}
		if i > 2 && len(client.Tools[i]) > 0 && !strings.Contains(history[0].Content, "arena.ch/programm: 1 finds") {
			t.Fatalf("call %d of the second run lacks the learned sources", i)
		}
	}
	// The note is about the previous run, never the one in progress.
	if !strings.Contains(last, "Last run took") || !strings.Contains(last, "1 new items") {
		t.Fatalf("second run's prompt lacks the last-run note:\n%s", last)
	}

	// News on an item reported before is news again: a launch slips, a
	// price drops twice.
	f.sllm.findings = []llm.Finding{{Key: "dune-part-three", Label: "Dune: Part Three", Source: 1}}
	client.Results = researchRound("Dune: Part Three moved to 18 Dec [1].")
	s.runSchedule(ctx, sch, timedRun)
	runs, _ = f.store.ListRuns(ctx, testUser.ID, sch.ID, 1)
	if runs[0].Status != schedule.StatusNew || strings.Join(runs[0].NewLabels, ",") != "Dune: Part Three" {
		t.Fatalf("run with news on a reported item = %+v", runs[0])
	}
	if notes, _ = f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 2 || notes[0].Title != "Cinema scout: 1 new" {
		t.Fatalf("notifications = %+v, want the update announced", notes)
	}
}

func peeqTools() []llm.Tool {
	return append(webTools(), llm.Tool{Type: "function", Function: llm.ToolFunction{Name: "peeq__search", Parameters: map[string]any{"type": "object"}}})
}

func TestScheduledRunRefusesToolsOutsideItsSet(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	var called []string
	call := func(id, name, args string) llm.ToolCall {
		return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunction{Name: name, Arguments: args}}
	}
	client := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{
			call("c1", "schedule_create", `{"prompt":"more films","interval":"daily"}`),
			call("c2", "peeq__search", `{"query":"films"}`),
		}},
		{Content: "Done."},
	}, Plain: "Nothing new."}
	deps := f.deps
	deps.LLM = client
	// peeq belongs to another account.
	deps.MCP = &fakeMCPService{ToolList: peeqTools(), Users: map[string][]string{"peeq": {"usr_other"}}, CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
		called = append(called, name)
		return "videos", nil
	}}
	s := newServer(deps)
	s.runSchedule(ctx, newRunnerSchedule(t, f), timedRun)
	if len(called) != 0 {
		t.Fatalf("a tool outside the run's set ran: %v", called)
	}
	for _, tool := range client.Tools[0] {
		if tool.Function.Name == "peeq__search" {
			t.Fatal("another account's MCP server was offered")
		}
	}
	if list, _ := f.store.ListSchedules(ctx, testUser.ID); len(list) != 1 {
		t.Fatalf("a scheduled run created a schedule: %+v", list)
	}
	// The round after the refused calls saw why they did not run.
	next := client.Histories[1]
	for _, m := range next[len(next)-2:] {
		if !strings.Contains(m.Content, "unknown tool") {
			t.Fatalf("the model was not told the tool is unknown: %+v", m)
		}
	}
}

func TestScheduledRunGetsTheUsersMCPServersAndMemory(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	if _, err := f.chat.UpsertUserMemory(ctx, testUser.ID, "Works as a platform engineer at a bank.", 1); err != nil {
		t.Fatal(err)
	}
	client := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "peeq__search", Arguments: `{"query":"agents","channel":"AI Engineer"}`}}}},
		{Content: "One new talk, written without the memory."},
	}, Plain: "One new talk on agents in banks [1]."}
	var called []string
	deps := f.deps
	deps.LLM = client
	deps.MCP = &fakeMCPService{
		ToolList:     peeqTools(),
		Users:        map[string][]string{"peeq": {testUser.ID}},
		Instructions: map[string]string{"peeq": "Search the AI Engineer channel unprompted."},
		CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
			called = append(called, name)
			return "one video", nil
		},
	}
	s := newServer(deps)
	sch := newRunnerSchedule(t, f)
	// A file format in the prompt brings no file tools, so no guidance for them.
	sch.Prompt = "summarize new videos as a PDF"
	s.runSchedule(ctx, sch, timedRun)
	if strings.Join(called, ",") != "peeq__search" {
		t.Fatalf("called = %v, want the user's peeq search", called)
	}
	system := client.Histories[0][0].Content
	if !strings.Contains(system, "Search the AI Engineer channel unprompted.") {
		t.Fatalf("the run's system prompt lacks peeq's guidance:\n%s", system)
	}
	if strings.Contains(system, "create_pdf_file") {
		t.Fatalf("the run's system prompt guides file tools it does not have:\n%s", system)
	}
	// The memory never reaches a call that offers tools, only the last,
	// tool-free one that writes the answer.
	last := len(client.Histories) - 1
	for i, history := range client.Histories {
		hasMemory := strings.Contains(history[0].Content, "platform engineer at a bank")
		if len(client.Tools[i]) > 0 && hasMemory {
			t.Fatalf("call %d offers tools and carries the memory", i)
		}
		if i == last && (len(client.Tools[i]) > 0 || !hasMemory) {
			t.Fatalf("the last call (tools %d, memory %v) is not the tool-free one with the memory", len(client.Tools[i]), hasMemory)
		}
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if answer := messages[len(messages)-1].Content; answer != "One new talk on agents in banks [1]." {
		t.Fatalf("answer = %q, want the tool-free call's", answer)
	}
}

func TestScheduledRunAnswersFromTheToolFreeCall(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: researchRound("DONE"), Plain: "Nothing new."}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	// The research round, the round that ends it, the answer: no more.
	if len(client.Histories) != 3 || len(client.Tools[2]) != 0 {
		t.Fatalf("model calls = %d, want two research rounds and one tool-free answer", len(client.Histories))
	}
	if !strings.Contains(client.Histories[0][0].Content, "reply with the single word DONE") {
		t.Fatal("the research rounds are not told to leave the answer to the final call")
	}
	if strings.Contains(client.Histories[2][0].Content, "single word DONE") {
		t.Fatal("the call that writes the answer is told to reply DONE")
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if answer := messages[len(messages)-1].Content; answer != "Nothing new." {
		t.Fatalf("answer = %q, want the tool-free call's", answer)
	}
}

func TestScheduledAnswerDropsImages(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: researchRound("DONE"),
		Plain: "New: a talk ![poster](https://evil.example/p.png?d=secret) and ![![x](y)](https://evil.example/?d=nested), see [the page](https://arena.ch/programm)."}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	answer := messages[len(messages)-1]
	if strings.Contains(answer.Content, "![") || strings.Contains(answer.Content, "evil.example") {
		t.Fatalf("answer still loads an image: %q", answer.Content)
	}
	if !strings.Contains(answer.Content, "a talk poster and") || !strings.Contains(answer.Content, "[the page](https://arena.ch/programm)") {
		t.Fatalf("answer lost its text or link: %q", answer.Content)
	}
	if strings.Contains(string(answer.ContentBlocks), "evil.example") {
		t.Fatalf("the answer's blocks still load an image: %s", answer.ContentBlocks)
	}
}

func TestScheduledRunOffersCategoryServersOnlyWhenTheyFit(t *testing.T) {
	for _, c := range []struct {
		name, category string
		err            error
		context7       bool
	}{
		{"recent news", "recent_news", nil, false},
		{"coding", "coding", nil, true},
		// A failed classification keeps every server rather than a guess.
		{"failed", "general", errors.New("gate timeout"), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			f := newScheduleFixture(t)
			client := &fakeToolChatClient{Results: researchRound("Nothing new."), ClassifyResult: c.category, ClassifyErr: c.err}
			deps := f.deps
			deps.LLM = client
			deps.MCP = &fakeMCPService{ToolList: peeqTools(), ToolCategories: map[string][]string{"context7__docs": {"coding"}}, Result: "page"}
			s := newServer(deps)
			s.runSchedule(ctx, newRunnerSchedule(t, f), timedRun)
			offered := map[string]bool{}
			for _, tool := range client.Tools[0] {
				offered[tool.Function.Name] = true
			}
			if offered["context7__docs"] != c.context7 || !offered["peeq__search"] || !offered["fetch__fetch"] {
				t.Fatalf("offered = %v", offered)
			}
		})
	}
}

// stallingChatClient answers part of a sentence, then stalls.
type stallingChatClient struct{ fakeChatClient }

func (*stallingChatClient) StreamChatWithTools(context.Context, []llm.Message, []llm.Tool, func(llm.StreamEvent) error) (llm.StreamResult, error) {
	return llm.StreamResult{Content: "One new film: A, and"}, llm.ErrStreamStalled
}

func TestScheduledRunWithACutOffAnswerRecordsNoFindings(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.sllm.findings = []llm.Finding{{Key: "a", Label: "A"}}
	f.deps.LLM = &stallingChatClient{}
	f.deps.MCP = &fakeMCPService{ToolList: webTools(), Result: "page"}
	s := newServer(f.deps)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusFailed || runs[0].Error != "the answer was cut off" || runs[0].MessageID == nil {
		t.Fatalf("run = %+v", runs)
	}
	if len(runs[0].NewLabels) != 0 || runs[0].NewCount != 0 {
		t.Fatalf("a cut-off answer recorded findings: %+v", runs[0])
	}
}

func TestCloseInterruptedRunNotesADanglingPrompt(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newServer(f.deps)
	thread, _ := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "s"})
	_, _ = f.chat.AddMessageWithAttachments(ctx, testUser.ID, thread.ID, chat.RoleUser, "find films", nil, nil, "")
	run := schedule.InterruptedRun{UserID: testUser.ID, ThreadID: thread.ID}
	s.closeInterruptedRun(ctx, run)
	s.closeInterruptedRun(ctx, run)
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, thread.ID)
	if len(messages) != 2 || !strings.Contains(messages[1].Content, "interrupted by a restart") {
		t.Fatalf("thread = %+v (one note, under the prompt)", messages)
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
		s.runSchedule(ctx, sch, timedRun)
		close(finished)
	}()
	<-blocking.started
	cancel()
	<-finished
	got, _ := f.store.GetSchedule(context.Background(), testUser.ID, sch.ID)
	messages, _, _ := f.chat.ListMessages(context.Background(), testUser.ID, lastRunThread(t, f, sch))
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
	s.runSchedule(ctx, sch, timedRun)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusTokenLimit || runs[0].NewCount != 1 || runs[0].Tokens < schedule.RunTokenBudget {
		t.Fatalf("run = %+v", runs)
	}
	// The forced final answer offered no tools.
	if len(client.Tools[len(client.Tools)-1]) != 0 {
		t.Fatal("the final answer after the budget still offered tools")
	}
}

// runPastTheTimeLimit runs sch with a model that never answers before the
// (shortened) hard limit.
func runPastTheTimeLimit(t *testing.T, f *scheduleFixture, sch schedule.Schedule) {
	t.Helper()
	f.deps.LLM = &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	s := newServer(f.deps)
	s.runTimeout = 50 * time.Millisecond
	s.runSchedule(context.Background(), sch, timedRun)
}

// seedTimedOutRun stores an earlier run of sch that went past the limit.
func seedTimedOutRun(t *testing.T, f *scheduleFixture, sch schedule.Schedule, start time.Time) {
	t.Helper()
	ctx := context.Background()
	runID, err := f.store.StartRun(ctx, testUser.ID, sch.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.FinishRun(ctx, testUser.ID, schedule.Run{ID: runID, Status: schedule.StatusTimeout}, start); err != nil {
		t.Fatal(err)
	}
}

// A timeout before the schedule was last edited or enabled again does not
// count: the user changed something since.
func TestScheduledRunTimeoutAfterAnEditStartsTheCountAnew(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	sch := newRunnerSchedule(t, f)
	seedTimedOutRun(t, f, sch, time.Now().Add(-time.Hour))
	runPastTheTimeLimit(t, f, sch)
	if got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID); !got.Enabled {
		t.Fatalf("a timeout from before the last edit disabled the schedule: %+v", got)
	}
}

// A tool still running at the soft deadline is cut too, so the final answer
// keeps its time.
func TestScheduledRunSoftDeadlineCutsARunningTool(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := researchClient("Nothing new.")
	deps := f.deps
	deps.LLM = client
	deps.MCP = &fakeMCPService{ToolList: webTools(), CallFunc: func(ctx context.Context, _ string, _ map[string]any) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	s := newServer(deps)
	s.runTimeout, s.runSoftDeadline = 10*time.Second, 300*time.Millisecond
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusNothingNew || runs[0].MessageID == nil {
		t.Fatalf("run = %+v, want an answer after the cut tool", runs)
	}
}

func TestScheduledRunFirstTimeoutKeepsTheSchedule(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	sch := newRunnerSchedule(t, f)
	runPastTheTimeLimit(t, f, sch)

	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusTimeout {
		t.Fatalf("run = %+v", runs)
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if !got.Enabled || got.DisabledReason != "" {
		t.Fatalf("one timeout disabled the schedule: %+v", got)
	}
	if notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 0 {
		t.Fatalf("one timeout notified: %+v", notes)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if len(messages) != 2 || !strings.Contains(messages[1].Content, "took longer than 5 minutes and was stopped") {
		t.Fatalf("thread = %+v", messages)
	}
}

// A schedule off by the time its run passes the limit (run by hand while off,
// or paused during the run: sch is the run's snapshot, still enabled) is not
// disabled and announced a second time.
func TestScheduledRunTimeoutOfAScheduleAlreadyOff(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	sch := newRunnerSchedule(t, f)
	seedTimedOutRun(t, f, sch, time.Now())
	if err := f.store.Disable(ctx, testUser.ID, sch.ID, schedule.DisabledTimeout, time.Now()); err != nil {
		t.Fatal(err)
	}
	runPastTheTimeLimit(t, f, sch)
	if notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 0 {
		t.Fatalf("a schedule already off was announced again: %+v", notes)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if len(messages) != 2 || messages[1].Content != "This scheduled run took longer than 5 minutes and was stopped." {
		t.Fatalf("thread = %+v", messages)
	}
}

func TestScheduledRunSecondTimeoutInARowDisablesTheSchedule(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	sch := newRunnerSchedule(t, f)
	seedTimedOutRun(t, f, sch, time.Now())
	runPastTheTimeLimit(t, f, sch)

	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if got.Enabled || got.DisabledReason != schedule.DisabledTimeout || got.DisabledAt == nil {
		t.Fatalf("schedule after the second timeout = %+v", got)
	}
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 || notes[0].Kind != schedule.KindScheduleDisabled || !strings.Contains(notes[0].Body, "Two runs in a row took longer than 5 minutes") {
		t.Fatalf("notifications = %+v", notes)
	}
	thread := lastRunThread(t, f, sch)
	if notes[0].ThreadID == nil || *notes[0].ThreadID != thread {
		t.Fatalf("notification thread = %v, want the run's %s", notes[0].ThreadID, thread)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, thread)
	if len(messages) != 2 || !strings.Contains(messages[1].Content, "disabled") {
		t.Fatalf("thread = %+v", messages)
	}
}

// pacedChatClient is a model whose tool rounds answer from Results, except
// round blockRound, which runs until its context ends. It records each call's
// inference metadata.
type pacedChatClient struct {
	*fakeToolChatClient
	blockRound int
	rounds     int
	metas      []inference.Metadata
}

func (c *pacedChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, tools []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	c.metas = append(c.metas, inference.MetadataFromContext(ctx))
	if len(tools) > 0 {
		c.rounds++
		if c.rounds == c.blockRound {
			c.Histories = append(c.Histories, history)
			c.Tools = append(c.Tools, tools)
			<-ctx.Done()
			return llm.StreamResult{}, ctx.Err()
		}
	}
	return c.fakeToolChatClient.StreamChatWithTools(ctx, history, tools, onEvent)
}

func pacedServer(t *testing.T, f *scheduleFixture, client *pacedChatClient, mcp *fakeMCPService) *server {
	t.Helper()
	deps := f.deps
	deps.LLM = client
	deps.MCP = mcp
	return newServer(deps)
}

func TestScheduledRunSoftDeadlineCutsTheRunningRound(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &pacedChatClient{
		fakeToolChatClient: &fakeToolChatClient{Results: researchRound("Nothing new."), Plain: "Nothing new."},
		blockRound:         2,
	}
	s := pacedServer(t, f, client, &fakeMCPService{ToolList: webTools(), Result: "page"})
	s.runTimeout, s.runSoftDeadline = 10*time.Second, 300*time.Millisecond
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)

	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusNothingNew || runs[0].MessageID == nil || runs[0].Rounds != 1 {
		t.Fatalf("run = %+v, want an answer after the cut round", runs)
	}
	if len(client.metas) != 3 || client.metas[2].Purpose != "chat_final" {
		t.Fatalf("calls = %+v, want two tool rounds and the final", client.metas)
	}
	if len(client.Tools[len(client.Tools)-1]) != 0 {
		t.Fatal("the final answer after the soft deadline still offered tools")
	}
}

func TestScheduledToolRoundsAskForTheLeastReasoning(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	// The model answers in its first round; the run still answers from the
	// final, never from a least-reasoning round.
	client := &pacedChatClient{fakeToolChatClient: &fakeToolChatClient{
		Results: []llm.StreamResult{{Content: "A quick draft."}},
		Plain:   "Nothing new.",
	}}
	s := pacedServer(t, f, client, &fakeMCPService{ToolList: webTools(), Result: "page"})
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)

	if len(client.metas) != 2 {
		t.Fatalf("calls = %+v", client.metas)
	}
	if round := client.metas[0]; round.Purpose != "chat_tool_round" || !round.LeastReasoning {
		t.Fatalf("tool round = %+v, want the least reasoning", round)
	}
	if final := client.metas[1]; final.Purpose != "chat_final" || final.LeastReasoning {
		t.Fatalf("final = %+v, want the turn's level", final)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if len(messages) != 2 || messages[1].Content != "Nothing new." {
		t.Fatalf("thread = %+v, want the final's answer", messages)
	}
}

func TestScheduledRunBoundsItsTools(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	search := func(id string) llm.ToolCall {
		return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunction{Name: "tavily__tavily_search",
			Arguments: `{"query":"films ` + id + `","max_results":10,"search_depth":"advanced","include_raw_content":true}`}}
	}
	var round1, round2 []llm.ToolCall
	round1 = append(round1, llm.ToolCall{ID: "f1", Type: "function", Function: llm.ToolCallFunction{Name: "fetch__fetch",
		Arguments: `{"url":"https://outnow.ch/Starts/Kinostarts","max_length":30000,"full_page":true}`}})
	round1 = append(round1, llm.ToolCall{ID: "f2", Type: "function", Function: llm.ToolCallFunction{Name: "fetch__fetch", Arguments: `{"url":"https://www.arena.ch/programm"}`}})
	for i := range 8 {
		round1 = append(round1, search(fmt.Sprintf("a%d", i)))
	}
	for i := range 4 {
		round2 = append(round2, search(fmt.Sprintf("b%d", i)))
	}
	client := &fakeToolChatClient{Results: []llm.StreamResult{{ToolCalls: round1}, {ToolCalls: round2}, {Content: "Done."}}, Plain: "Nothing new."}
	var mu sync.Mutex
	calls := map[string][]map[string]any{}
	mcp := &fakeMCPService{ToolList: webTools(), CallFunc: func(_ context.Context, name string, args map[string]any) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls[name] = append(calls[name], args)
		return strings.Repeat("x", 50<<10), nil
	}}
	deps := f.deps
	deps.LLM, deps.MCP = client, mcp
	newServer(deps).runSchedule(ctx, newRunnerSchedule(t, f), timedRun)

	if n := len(calls["tavily__tavily_search"]); n != 10 {
		t.Fatalf("tavily calls = %d, want the run's limit of 10", n)
	}
	for _, args := range calls["tavily__tavily_search"] {
		if args["max_results"] != float64(5) || args["search_depth"] != "basic" || args["include_raw_content"] != nil {
			t.Fatalf("search args = %v, want 5 basic results without raw content", args)
		}
	}
	for _, args := range calls["fetch__fetch"] {
		// A size the model set is lowered; a missing one keeps the tool's smaller default.
		want := any(float64(8000))
		if args["url"] == "https://www.arena.ch/programm" {
			want = nil
		}
		if args["max_length"] != want {
			t.Fatalf("fetch args = %v, want max_length %v", args, want)
		}
	}
	if n := len(calls["fetch__fetch"]); n != 2 {
		t.Fatalf("fetch calls = %d", n)
	}
	var limited int
	for _, m := range client.Histories[2] {
		if m.Role != "tool" {
			continue
		}
		// 12 KB of output, plus the [n] source label added after the cap.
		if len(m.Content) > 12<<10+256 {
			t.Fatalf("a tool result of %d bytes entered the history", len(m.Content))
		}
		if strings.Contains(m.Content, "search limit of this run") {
			limited++
		}
	}
	if limited != 2 {
		t.Fatalf("calls past the limit = %d, want 2", limited)
	}
}

func TestScheduledRunFailureLeavesANote(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.deps.LLM = &fakeChatClient{StreamErr: errors.New("upstream down")}
	s := newServer(f.deps)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusFailed || runs[0].Error == "" {
		t.Fatalf("run = %+v", runs)
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if !got.Enabled {
		t.Fatal("a failed run disabled the schedule")
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if len(messages) != 2 || !strings.HasPrefix(messages[1].Content, "This scheduled run failed") {
		t.Fatalf("thread = %+v", messages)
	}
	if notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10); len(notes) != 0 {
		t.Fatalf("a failed run notified: %+v", notes)
	}
}

func TestScheduledRunKeepsAThreadTheUserAskedIn(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newRunnerServer(t, f, researchClient("Dune: Part Three [1]."))
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	asked := lastRunThread(t, f, sch)
	if _, err := f.chat.AddMessageWithAttachments(ctx, testUser.ID, asked, chat.RoleUser, "When does it open?", nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	s.runSchedule(ctx, sch, timedRun)
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, asked); !found {
		t.Fatal("a run replaced a thread the user asked something in")
	}
	if lastRunThread(t, f, sch) == asked {
		t.Fatal("the second run wrote into the thread the user took up")
	}
}

// A nothing-new run's thread the user starred while the run went on is theirs:
// dropEmptyRunThread keeps it and reports it taken up, an untouched one goes.
func TestNothingNewRunKeepsAThreadTheUserTookUp(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newRunnerServer(t, f, researchClient("Dune: Part Three [1]."))
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	first := lastRunThread(t, f, sch)

	starred, err := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "Run", ScheduleID: &sch.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.chat.SetThreadStarred(ctx, testUser.ID, starred.ID, true); err != nil {
		t.Fatal(err)
	}
	if dropped, takenUp := s.dropEmptyRunThread(ctx, testUser.ID, starred.ID); dropped || !takenUp {
		t.Fatalf("starred run thread: dropped %v, taken up %v", dropped, takenUp)
	}
	if dropped, takenUp := s.dropEmptyRunThread(ctx, testUser.ID, first); !dropped || takenUp {
		t.Fatalf("untouched run thread: dropped %v, taken up %v", dropped, takenUp)
	}
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, starred.ID); !found {
		t.Fatal("the starred run thread is gone")
	}
}

// A manual run with nothing new keeps its thread, and the last news, read
// or not, stays beside it.
func TestManualNothingNewRunKeepsBothThreads(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.sllm.findings = []llm.Finding{{Key: "dune-part-three", Label: "Dune: Part Three", Source: 1}}
	client := researchClient("Dune: Part Three [1].")
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	news := lastRunThread(t, f, sch)
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 {
		t.Fatalf("notifications = %+v", notes)
	}
	if err := f.store.MarkRead(ctx, testUser.ID, notes[0].ID); err != nil {
		t.Fatal(err)
	}

	f.sllm.findings = []llm.Finding{}
	client.Results, client.Plain = researchRound("Nothing new [1]."), "Nothing new [1]."
	s.runSchedule(ctx, sch, manualRun)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 2 || runs[0].Status != schedule.StatusNothingNew || runs[0].ThreadID == nil || *runs[0].ThreadID == news {
		t.Fatalf("runs = %+v, want a nothing-new run with a thread of its own", runs)
	}
	for _, id := range []string{news, *runs[0].ThreadID} {
		if _, found, _ := f.chat.GetThread(ctx, testUser.ID, id); !found {
			t.Fatalf("thread %s is gone", id)
		}
	}
}

func TestFailedScheduledRunStillReplacesTheThreadBefore(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newRunnerServer(t, f, researchClient("Dune: Part Three [1]."))
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	first := lastRunThread(t, f, sch)

	f.deps.LLM = &fakeChatClient{StreamErr: errors.New("upstream down")}
	newServer(f.deps).runSchedule(ctx, sch, timedRun)
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, first); found {
		t.Fatal("a failed run kept the untouched thread before it")
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, lastRunThread(t, f, sch))
	if len(messages) != 2 || !strings.HasPrefix(messages[1].Content, "This scheduled run failed") {
		t.Fatalf("failed run's thread = %+v", messages)
	}
}

func TestDeletingAScheduleTakesItsUntouchedRunThreads(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newRunnerServer(t, f, researchClient("Dune: Part Three [1]."))
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	untouched := lastRunThread(t, f, sch)
	asked, err := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "Asked", ScheduleID: &sch.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"find new films", "When does it open?"} {
		if _, err := f.chat.AddMessageWithAttachments(ctx, testUser.ID, asked.ID, chat.RoleUser, content, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}

	if rec := f.do(t, http.MethodDelete, "/api/schedules/"+sch.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, untouched); found {
		t.Fatal("deleting the schedule kept its untouched run thread")
	}
	if got, found, _ := f.chat.GetThread(ctx, testUser.ID, asked.ID); !found || got.ScheduleID != nil {
		t.Fatalf("thread the user asked in = %+v (found %v), want it kept and unlinked", got, found)
	}
}

func TestDeletingAScheduleKeepsTheThreadOfARunInProgress(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	sch := newRunnerSchedule(t, f)
	runID, err := f.store.StartRun(ctx, testUser.ID, sch.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	running, err := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: sch.Name, ScheduleID: &sch.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.AttachRunThread(ctx, testUser.ID, runID, running.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.chat.AddMessageWithAttachments(ctx, testUser.ID, running.ID, chat.RoleUser, sch.Prompt, nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	if rec := f.do(t, http.MethodDelete, "/api/schedules/"+sch.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, running.ID); !found {
		t.Fatal("deleting the schedule took the thread its running run writes into")
	}
}

func TestPruneRunThreadsDropsOldUntouchedOnes(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newRunnerServer(t, f, researchClient("Dune: Part Three [1]."))
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	untouched := lastRunThread(t, f, sch)
	now := time.Now()

	s.pruneRunThreads(ctx, now)
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, untouched); !found {
		t.Fatal("a prune dropped a run thread younger than a month")
	}
	asked, err := f.chat.CreateThread(ctx, testUser.ID, chat.CreateThreadInput{Title: "Asked", ScheduleID: &sch.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"find new films", "When does it open?"} {
		if _, err := f.chat.AddMessageWithAttachments(ctx, testUser.ID, asked.ID, chat.RoleUser, content, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`UPDATE threads SET created_at = ?`, now.Add(-runThreadRetention-time.Hour).UTC().Format(time.DateTime)); err != nil {
		t.Fatal(err)
	}

	// An enabled schedule that runs less than monthly keeps its newest answer.
	s.pruneRunThreads(ctx, now)
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, untouched); !found {
		t.Fatal("a prune dropped an enabled schedule's newest run thread")
	}
	if _, err := f.db.Exec(`UPDATE schedules SET enabled = 0`); err != nil {
		t.Fatal(err)
	}
	s.pruneRunThreads(ctx, now)
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, untouched); found {
		t.Fatal("a prune kept a paused schedule's untouched run thread older than a month")
	}
	if _, found, _ := f.chat.GetThread(ctx, testUser.ID, asked.ID); !found {
		t.Fatal("a prune dropped a run thread the user asked something in")
	}
}

// A store failure while replacing or purging run threads only logs: the run
// and the worker carry on, and every thread still gets its try.
func TestRunThreadCleanupSurvivesStoreFailures(t *testing.T) {
	ctx := context.Background()
	store := &fakeThreadStore{RunThreadsErr: errors.New("db down")}
	s := newServer(Deps{Thread: store})
	s.replaceRunThreads(ctx, testUser.ID, "s1", "keep")
	s.pruneRunThreads(ctx, time.Now())
	if dropped, takenUp := s.dropEmptyRunThread(ctx, testUser.ID, "a"); dropped || takenUp || len(store.DeletedThreads) != 0 {
		t.Fatalf("deleted %v after the listing failed (taken up %v)", store.DeletedThreads, takenUp)
	}

	store.RunThreadsErr = nil
	store.RunThreads = []chat.ThreadRef{{UserID: testUser.ID, ThreadID: "a"}, {UserID: testUser.ID, ThreadID: "b"}}
	s.replaceRunThreads(ctx, testUser.ID, "s1", "keep")
	s.pruneRunThreads(ctx, time.Now())
	if want := []string{"a", "b", "a", "b"}; !slices.Equal(store.DeletedThreads, want) {
		t.Fatalf("deleted %v, want %v", store.DeletedThreads, want)
	}
	if _, takenUp := s.dropEmptyRunThread(ctx, testUser.ID, "taken-up"); !takenUp {
		t.Fatal("a thread outside the untouched ones is not reported taken up")
	}
	if dropped, _ := s.dropEmptyRunThread(ctx, testUser.ID, "a"); !dropped {
		t.Fatal("an untouched thread was not dropped")
	}
	store.DeleteThreadErr = errors.New("locked")
	s.replaceRunThreads(ctx, testUser.ID, "s1", "keep")
	s.pruneRunThreads(ctx, time.Now())
	if dropped, takenUp := s.dropEmptyRunThread(ctx, testUser.ID, "a"); dropped || takenUp || len(store.DeletedThreads) != 5 {
		t.Fatalf("failed deletes recorded: %v (dropped %v, taken up %v)", store.DeletedThreads, dropped, takenUp)
	}
}

func TestScheduleWorkerStartsDueRuns(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := researchClient("Nothing new.")
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
	client := researchClient("Nothing new.")
	f.deps.LLM = client
	f.deps.MCP = &fakeMCPService{ToolList: webTools(), Result: "page"}
	f.handler = newAuthenticatedServer(t, f.deps)
	// Nothing new, yet the run the user asked for keeps its thread.
	f.sllm.findings = []llm.Finding{}
	sch := newRunnerSchedule(t, f)
	if rec := f.do(t, "POST", "/api/schedules/"+sch.ID+"/run", ""); rec.Code != 202 {
		t.Fatalf("run now = %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, _ := f.store.ListRuns(context.Background(), testUser.ID, sch.ID, 5)
		if len(runs) == 1 && runs[0].Status != schedule.StatusRunning {
			if runs[0].Status != schedule.StatusNothingNew || runs[0].ThreadID == nil {
				t.Fatalf("run = %+v, want nothing new with its thread kept", runs[0])
			}
			if _, found, _ := f.chat.GetThread(context.Background(), testUser.ID, *runs[0].ThreadID); !found {
				t.Fatal("run now dropped its thread")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run now did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// "Run the cinema scout now" starts a run from the chat, as the Run now
// button does: one at a time per user.
func TestScheduleToolRunStartsARun(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	s := newRunnerServer(t, f, researchClient("Nothing new."))
	tools := scheduleToolRunner{s: s}
	sch := newRunnerSchedule(t, f)

	if out := tools.Run(ctx, testUser, map[string]any{}); !strings.Contains(out, "id is required") {
		t.Fatalf("run without id = %q", out)
	}
	if out := tools.Run(ctx, testUser, map[string]any{"id": "nope"}); !strings.Contains(out, "No schedule has that id") {
		t.Fatalf("run unknown = %q", out)
	}
	// Another run of the user's, or other users' runs in every slot, hold it
	// back, each with its own reason; a lost claim hands the user's slot back.
	for _, c := range []struct {
		holders []string
		want    string
	}{
		{[]string{testUser.ID}, "Not started: another of your scheduled runs is in progress"},
		{[]string{"u_a", "u_b"}, "Not started: loom is already running as many scheduled runs"},
	} {
		for _, id := range c.holders {
			s.runs.admit(id)
		}
		if out := tools.Run(ctx, testUser, map[string]any{"id": sch.ID}); !strings.Contains(out, c.want) {
			t.Fatalf("run with %v busy = %q", c.holders, out)
		}
		for _, id := range c.holders {
			s.runs.done(id)
		}
	}
	if ok, _ := f.store.Claim(ctx, testUser.ID, sch.ID, time.Now()); !ok {
		t.Fatal("claim failed")
	}
	if out := tools.Run(ctx, testUser, map[string]any{"id": sch.ID}); !strings.Contains(out, "this schedule is already running") {
		t.Fatalf("run of a running schedule = %q", out)
	}
	if err := f.store.Release(ctx, testUser.ID, sch.ID, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if !s.runs.admit(testUser.ID) {
		t.Fatal("the lost claim kept the user's slot")
	}
	s.runs.done(testUser.ID)

	// A schedule loom turned off runs on request, with a warning, and keeps
	// its thread with nothing new.
	f.sllm.findings = []llm.Finding{}
	if err := f.store.Disable(ctx, testUser.ID, sch.ID, schedule.DisabledTimeout, time.Now()); err != nil {
		t.Fatal(err)
	}
	out := tools.Run(ctx, testUser, map[string]any{"id": sch.ID})
	if !strings.HasPrefix(out, `Started a run of "Cinema scout"`) || !strings.Contains(out, "new thread") || !strings.Contains(out, "stays off") {
		t.Fatalf("run = %q", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
		if len(runs) == 1 && runs[0].Status == schedule.StatusNothingNew && runs[0].ThreadID != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the started run did not finish: %+v", runs)
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
	// "Disable the cinema scout" is an update to enabled=false; "enable it"
	// turns it on again, also after loom disabled it for its time.
	if out := tools.Update(ctx, testUser, "", map[string]any{"id": id, "enabled": false}); !strings.Contains(out, "next run none, off.") {
		t.Fatalf("disable = %q", out)
	}
	if err := f.store.Disable(ctx, testUser.ID, id, schedule.DisabledTimeout, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out := tools.List(ctx, testUser); !strings.Contains(out, "off, disabled automatically: two runs in a row") {
		t.Fatalf("list after a timeout = %q", out)
	}
	if out := tools.Update(ctx, testUser, "", map[string]any{"id": id, "enabled": true}); !strings.Contains(out, ", on.") {
		t.Fatalf("enable = %q", out)
	}
	if got, _ := f.store.GetSchedule(ctx, testUser.ID, id); !got.Enabled || got.DisabledReason != "" || got.NextRunAt == nil {
		t.Fatalf("schedule after enable = %+v", got)
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

// promptlessStore refuses to store a message.
type promptlessStore struct{ *chat.Store }

func (promptlessStore) AddMessageWithAttachments(context.Context, string, string, chat.Role, string, json.RawMessage, json.RawMessage, string) (chat.Message, error) {
	return chat.Message{}, errors.New("database is locked")
}

func TestScheduledRunDropsAThreadItCannotFill(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.deps.Thread = promptlessStore{f.chat}
	s := newServer(f.deps)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch, timedRun)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusFailed || runs[0].Error != "could not store the prompt" || runs[0].ThreadID != nil {
		t.Fatalf("run = %+v", runs)
	}
	if threads, _ := f.chat.ListThreads(ctx, testUser.ID, chat.ListThreadsOptions{}); len(threads) != 0 {
		t.Fatalf("an empty run thread stayed: %+v", threads)
	}
}

func TestRejectWhileRunningReportsAStoreFailure(t *testing.T) {
	f := newScheduleFixture(t)
	s := newServer(f.deps)
	scheduleID := "s1"
	_ = f.db.Close()
	rec := httptest.NewRecorder()
	if !s.rejectWhileRunning(rec, httptest.NewRequest(http.MethodPost, "/", nil), testUser.ID, chat.Thread{ID: "t", ScheduleID: &scheduleID}) {
		t.Fatal("a failed check let the message through")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestScheduleNamer(t *testing.T) {
	f := newScheduleFixture(t)
	s := newServer(f.deps)
	if name, err := (scheduleNamer{s: s}).NameSchedule(context.Background(), testUser.ID, "find films"); err != nil || name != "Cinema scout" {
		t.Fatalf("name = %q, %v", name, err)
	}
}
