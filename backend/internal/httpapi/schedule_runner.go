package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/turn"
)

const (
	// scheduleTick is how often the worker looks for due runs.
	scheduleTick = time.Minute
	// maxConcurrentScheduleRuns bounds the runs in flight across all users.
	maxConcurrentScheduleRuns = 2
	// schedulePruneEvery is how often old notifications and stale memory go.
	schedulePruneEvery = time.Hour
	// findingsTimeout bounds the gate call that lists a run's findings.
	findingsTimeout = time.Minute
)

// scheduleRuns admits scheduled runs: at most maxConcurrentScheduleRuns in
// flight, and one per user.
type scheduleRuns struct {
	mu     sync.Mutex
	active map[string]bool
	slots  int
}

func newScheduleRuns() *scheduleRuns {
	return &scheduleRuns{active: map[string]bool{}, slots: maxConcurrentScheduleRuns}
}

// admit takes a slot for the user; false when the user already runs one or
// every slot is taken.
func (r *scheduleRuns) admit(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[userID] || len(r.active) >= r.slots {
		return false
	}
	r.active[userID] = true
	return true
}

func (r *scheduleRuns) done(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, userID)
}

func (s *server) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// scheduleRunTimeout is schedule.RunTimeout unless a test shortened it.
func (s *server) scheduleRunTimeout() time.Duration {
	if s.runTimeout > 0 {
		return s.runTimeout
	}
	return schedule.RunTimeout
}

// ScheduleWorker starts the scheduled runs that are due.
type ScheduleWorker struct {
	s *server
}

// Run starts due runs every minute until ctx ends. A claim a previous process
// left behind is cleared first; a schedule whose run time passed while loom
// was down runs once, never once per missed slot.
func (w *ScheduleWorker) Run(ctx context.Context) {
	s := w.s
	if s.schedules == nil {
		return
	}
	interrupted, err := s.schedules.ResetInterrupted(ctx)
	if err != nil {
		slog.Warn("reset interrupted schedule runs failed", "err", err)
	}
	for _, run := range interrupted {
		s.closeInterruptedRun(ctx, run)
	}
	ticker := time.NewTicker(scheduleTick)
	defer ticker.Stop()
	var pruned time.Time
	for {
		w.startDue(ctx)
		if now := s.clock(); now.Sub(pruned) >= schedulePruneEvery {
			pruned = now
			if err := s.schedules.Prune(ctx, now); err != nil {
				slog.Warn("prune schedule data failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// startDue claims and starts every due schedule a slot admits; the rest wait
// for the next tick.
func (w *ScheduleWorker) startDue(ctx context.Context) {
	s := w.s
	now := s.clock()
	due, err := s.schedules.DueSchedules(ctx, now, 20)
	if err != nil {
		slog.Warn("list due schedules failed", "err", err)
		return
	}
	for _, sch := range due {
		if !s.runs.admit(sch.UserID) {
			continue
		}
		claimed, err := s.schedules.ClaimDue(ctx, sch.UserID, sch.ID, now, schedule.NextRun(sch, now))
		if err != nil || !claimed {
			s.runs.done(sch.UserID)
			if err != nil {
				slog.Warn("claim schedule failed", "schedule_id", sch.ID, "err", err)
			}
			continue
		}
		s.startScheduleRun(ctx, sch)
	}
}

// startScheduleRun runs a claimed and admitted schedule in the background.
func (s *server) startScheduleRun(ctx context.Context, sch schedule.Schedule) {
	release := func() {
		detached := context.WithoutCancel(ctx)
		current, err := s.schedules.GetSchedule(detached, sch.UserID, sch.ID)
		if err != nil {
			current = sch
		}
		now := s.clock()
		if err := s.schedules.Release(detached, sch.UserID, sch.ID, now, schedule.NextRun(current, now)); err != nil {
			slog.Warn("release schedule failed", "schedule_id", sch.ID, "err", err)
		}
		s.runs.done(sch.UserID)
	}
	if !s.background.Spawn(s.lifetime, "schedule:"+sch.ID, func(groupCtx context.Context) {
		defer release()
		s.runSchedule(groupCtx, sch)
	}) {
		release()
	}
}

// discardEmitter drops a headless run's stream events: nobody follows them.
type discardEmitter struct{}

func (discardEmitter) SendJSON(string, any) error { return nil }

// observations collects a run's tool outcomes from the loop's goroutines.
type observations struct {
	mu  sync.Mutex
	all []schedule.ToolOutcome
}

func (o *observations) add(t turn.ToolObservation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.all = append(o.all, schedule.ToolOutcome{Kind: t.Tool, URL: t.URL, Query: t.Query, OK: t.OK, FellBack: t.FellBack, ResultURLs: t.ResultURLs})
}

func (o *observations) list() []schedule.ToolOutcome {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.all
}

// runSchedule carries out one run of a claimed schedule: its prompt as a
// headless turn in the schedule's thread, then the findings, the memory, the
// run record and, when there is news, a notification.
func (s *server) runSchedule(ctx context.Context, sch schedule.Schedule) {
	detached := context.WithoutCancel(ctx)
	start := s.clock()
	// The memory is read before this run is recorded, so its last run is the
	// previous one, not this run in progress.
	memory, err := s.schedules.LoadMemory(detached, sch.UserID, sch.ID)
	if err != nil {
		slog.Warn("load schedule memory failed", "schedule_id", sch.ID, "err", err)
	}
	runID, err := s.schedules.StartRun(detached, sch.UserID, sch.ID, start)
	if err != nil {
		slog.Warn("start schedule run failed", "schedule_id", sch.ID, "err", err)
		return
	}
	run := schedule.Run{ID: runID, Status: schedule.StatusFailed}
	// noteThread is set while the thread holds this run's prompt without a
	// reply; whatever ends the run then leaves a line saying why.
	var noteThread string
	finish := func() {
		run.DurationMS = s.clock().Sub(start).Milliseconds()
		if err := s.schedules.FinishRun(detached, sch.UserID, run, s.clock()); err != nil {
			slog.Warn("finish schedule run failed", "schedule_id", sch.ID, "err", err)
		}
	}
	defer func() {
		if p := recover(); p != nil {
			slog.Error("panic recovered in scheduled run", "err", p, "schedule_id", sch.ID, "stack", string(debug.Stack()))
			run.Status, run.Error = schedule.StatusFailed, "internal error"
			if noteThread != "" {
				s.persistRunNote(detached, sch.UserID, noteThread, "This scheduled run failed: internal error")
			}
		}
		finish()
	}()

	user, found, err := s.users.FindByID(detached, sch.UserID)
	if err != nil || !found {
		run.Error = "the account is gone"
		return
	}
	thread, err := s.scheduleThread(detached, user, sch)
	if err != nil {
		slog.Warn("schedule thread failed", "schedule_id", sch.ID, "err", err)
		run.Error = "could not open the schedule's thread"
		return
	}
	previousActivity := thread.LastMessageAt
	userMessage, err := s.thread.AddMessageWithAttachments(detached, user.ID, thread.ID, chat.RoleUser, sch.Prompt, nil, nil, "")
	if err != nil {
		slog.Warn("persist schedule prompt failed", "schedule_id", sch.ID, "err", err)
		run.Error = "could not store the prompt"
		return
	}
	noteThread = thread.ID

	runCtx, cancel := context.WithTimeout(ctx, s.scheduleRunTimeout())
	defer cancel()
	usageTotal := llm.NewUsageAccumulator()
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: thread.ID}
	streamCtx := llm.WithInferenceMetadata(llm.WithUsageAccumulator(runCtx, usageTotal), inference)
	turnCtx := llm.WithUsageAccumulator(llm.WithInferenceMetadata(detached, inference), usageTotal)
	costs := s.engine.NewCostSettler(user, userMessage.ID, usageTotal)
	defer func() {
		costs.Settle(detached)
		total := usageTotal.Total()
		run.Tokens = int64(total.TotalTokens)
		run.CostNanoUSD, _ = usageTotal.Cost()
	}()
	var seen observations
	turnRun := s.engine.Prepare(turn.RunConfig{
		Stream:      discardEmitter{},
		User:        user,
		Thread:      thread,
		UserMessage: userMessage,
		Usage:       usageTotal,
		Start:       start,
		Scheduled: &turn.ScheduledRun{
			Tools:        turn.ScheduledRunTools(),
			TokenBudget:  schedule.RunTokenBudget,
			SoftDeadline: start.Add(schedule.RunSoftDeadline),
			Memory:       memory.Block(schedule.MemoryTokenBudget),
			Observe:      seen.add,
			Now:          s.clock,
		},
	}, turn.PrepareInput{StreamCtx: streamCtx, TurnCtx: turnCtx, ReqCtx: detached})
	result, loopErr := turnRun.RunAssistantLoop(streamCtx)
	run.Rounds = result.Rounds

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		run.Status, run.Error = schedule.StatusTimeout, "the run took longer than the time limit"
		noteThread = ""
		s.disableAfterTimeout(detached, user, sch, thread)
		return
	}
	// failWith ends a run without an answer: a line in the thread says why,
	// and the thread keeps its place in the recents.
	failWith := func(message, note string) {
		run.Error = message
		noteThread = ""
		s.persistRunNote(detached, user.ID, thread.ID, note)
		s.restoreActivity(detached, user.ID, thread.ID, previousActivity)
	}
	if ctx.Err() != nil {
		// Shutdown: the run is not the schedule's fault.
		failWith("interrupted by a shutdown", "This scheduled run was interrupted by a restart.")
		return
	}
	if loopErr != nil || strings.TrimSpace(result.Content) == "" {
		message := "the run produced no answer"
		if loopErr != nil {
			message = turn.StreamFailureMessage(loopErr, result, "schedule", thread.ID)
		}
		failWith(message, "This scheduled run failed: "+message)
		return
	}
	assistant, err := turnRun.PersistAssistantTurn(detached, &result)
	if err != nil {
		slog.Warn("persist scheduled answer failed", "schedule_id", sch.ID, "err", err)
		failWith("could not store the answer", "This scheduled run failed: its answer could not be stored.")
		return
	}
	noteThread = ""
	costs.SetAssistant(assistant)
	run.MessageID = &assistant.ID
	if result.Interrupted {
		// A stalled answer is kept for the reader, but its cut-off list is no
		// record of what is new: the next run reports the rest.
		run.Error = "the answer was cut off"
		s.restoreActivity(detached, user.ID, thread.ID, previousActivity)
		return
	}

	// The extraction gets its own deadline, and ends with the server: the
	// run's own limit is spent by now.
	extractCtx, cancelExtract := context.WithTimeout(
		llm.WithUsageAccumulator(llm.WithInferenceMetadata(ctx, inference), usageTotal), findingsTimeout)
	defer cancelExtract()
	fresh := s.recordFindings(extractCtx, user.ID, sch, runID, result, memory.Findings, seen.list())
	run.NewCount = len(fresh)
	switch {
	case result.LimitHit == turn.LimitTokens:
		run.Status = schedule.StatusTokenLimit
	case len(fresh) > 0:
		run.Status = schedule.StatusNew
	default:
		run.Status = schedule.StatusNothingNew
	}
	if len(fresh) == 0 {
		s.restoreActivity(detached, user.ID, thread.ID, previousActivity)
		return
	}
	labels := make([]string, len(fresh))
	for i, f := range fresh {
		labels[i] = f.Label
	}
	s.notify(detached, user.ID, schedule.Notification{
		Kind:       schedule.KindScheduleRun,
		ScheduleID: &sch.ID,
		ThreadID:   &thread.ID,
		MessageID:  &assistant.ID,
		Title:      fmt.Sprintf("%s: %d new", sch.Name, len(fresh)),
		Body:       strings.Join(labels, " · "),
	})
}

// recordFindings extracts the items the answer reported, keeps the new ones
// and teaches the schedule's memory which sources and searches led to them.
func (s *server) recordFindings(ctx context.Context, userID string, sch schedule.Schedule, runID string, result turn.LoopResult, known []schedule.Finding, seen []schedule.ToolOutcome) []schedule.Finding {
	keys := make([]string, len(known))
	for i, f := range known {
		keys[i] = f.Key
	}
	items, err := s.scheduleLLM.ExtractFindings(ctx, sch.Prompt, result.Content, keys)
	if err != nil {
		slog.Warn("extract findings failed", "schedule_id", sch.ID, "err", err)
	}
	urls := result.SourceURLs()
	found := make([]schedule.Finding, 0, len(items))
	for _, item := range items {
		found = append(found, schedule.Finding{Key: item.Key, Label: item.Label, SourceURL: urls[item.Source]})
	}
	now := s.clock()
	fresh, err := s.schedules.AddFindings(context.WithoutCancel(ctx), userID, sch.ID, runID, found, now)
	if err != nil {
		slog.Warn("store findings failed", "schedule_id", sch.ID, "err", err)
		return nil
	}
	if err := s.schedules.RecordRun(context.WithoutCancel(ctx), userID, sch.ID, seen, fresh, now); err != nil {
		slog.Warn("record schedule memory failed", "schedule_id", sch.ID, "err", err)
	}
	return fresh
}

// scheduleThread returns the schedule's thread, creating it on the first run
// or after the user deleted it.
func (s *server) scheduleThread(ctx context.Context, user auth.User, sch schedule.Schedule) (chat.Thread, error) {
	if sch.ThreadID != nil {
		thread, found, err := s.thread.GetThread(ctx, user.ID, *sch.ThreadID)
		if err != nil {
			return chat.Thread{}, err
		}
		if found {
			return thread, nil
		}
	}
	thread, err := s.thread.CreateThread(ctx, user.ID, chat.CreateThreadInput{Title: sch.Name})
	if err != nil {
		return chat.Thread{}, err
	}
	if err := s.schedules.SetThread(ctx, user.ID, sch.ID, thread.ID); err != nil {
		return chat.Thread{}, err
	}
	thread.ScheduleID = &sch.ID
	return thread, nil
}

// disableAfterTimeout turns a schedule off whose run went past the time limit,
// says so in its thread and tells the user.
func (s *server) disableAfterTimeout(ctx context.Context, user auth.User, sch schedule.Schedule, thread chat.Thread) {
	minutes := int(schedule.RunTimeout / time.Minute)
	if err := s.schedules.Disable(ctx, user.ID, sch.ID, schedule.DisabledTimeout, s.clock()); err != nil {
		slog.Warn("disable schedule failed", "schedule_id", sch.ID, "err", err)
	}
	s.persistRunNote(ctx, user.ID, thread.ID, fmt.Sprintf("This scheduled run took longer than %d minutes, so the schedule was disabled.", minutes))
	s.notify(ctx, user.ID, schedule.Notification{
		Kind:       schedule.KindScheduleDisabled,
		ScheduleID: &sch.ID,
		ThreadID:   &thread.ID,
		Title:      sch.Name + " was disabled",
		Body:       fmt.Sprintf("The run took longer than %d minutes. Open the schedule to change it or enable it again.", minutes),
	})
}

// closeInterruptedRun notes under a prompt the previous process left without
// a reply. A run that stopped before it stored its prompt left none.
func (s *server) closeInterruptedRun(ctx context.Context, run schedule.InterruptedRun) {
	last, err := s.thread.ListRecentMessages(ctx, run.UserID, run.ThreadID, 1)
	if err != nil || len(last) == 0 || last[0].Role != chat.RoleUser {
		return
	}
	s.persistRunNote(ctx, run.UserID, run.ThreadID, "This scheduled run was interrupted by a restart.")
}

// persistRunNote ends a run that has no answer with a line saying why, so the
// thread never holds a prompt without a reply.
func (s *server) persistRunNote(ctx context.Context, userID, threadID, note string) {
	if _, err := s.thread.AddMessageWithCitations(ctx, userID, threadID, chat.RoleAssistant, note, chat.MessageTokenUsage{}, nil, nil, nil, nil); err != nil {
		slog.Warn("persist scheduled run note failed", "thread_id", threadID, "err", err)
	}
}

// restoreActivity keeps a run without news from moving its thread up the
// recents.
func (s *server) restoreActivity(ctx context.Context, userID, threadID string, at *time.Time) {
	if err := s.thread.SetLastMessageAt(ctx, userID, threadID, at); err != nil {
		slog.Warn("restore thread activity failed", "thread_id", threadID, "err", err)
	}
}

func (s *server) notify(ctx context.Context, userID string, n schedule.Notification) {
	if err := s.schedules.CreateNotification(ctx, userID, n); err != nil {
		slog.Warn("create notification failed", "err", err)
	}
}

// scheduleNamer names a schedule from its prompt with the title generator.
type scheduleNamer struct{ s *server }

func (n scheduleNamer) NameSchedule(ctx context.Context, userID, prompt string) (string, error) {
	language := ""
	if user, found, err := n.s.users.FindByID(ctx, userID); err == nil && found {
		language = user.ResponseLanguageName()
	}
	return n.s.llm.GenerateThreadTitle(ctx, prompt, "", language)
}

// scheduleThreadTitles renames a schedule's thread with the schedule.
type scheduleThreadTitles struct{ s *server }

func (t scheduleThreadTitles) SetThreadTitle(ctx context.Context, userID, threadID, title string) error {
	_, _, err := t.s.thread.UpdateThread(ctx, userID, threadID, chat.UpdateThreadInput{Title: &title})
	return err
}
