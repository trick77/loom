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
	// runThreadRetention is how long a run thread the user never took up
	// outlives its run, e.g. the last one of a paused schedule.
	runThreadRetention = 30 * 24 * time.Hour
	// runThreadPruneBatch bounds the run threads one prune deletes; the rest
	// go on the next.
	runThreadPruneBatch = 50
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
	return r.take(userID) == nil
}

// take takes a slot for the user, or says why not: errOwnRunActive when the
// user already runs one, errRunBusy when every slot is taken.
func (r *scheduleRuns) take(userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[userID] {
		return errOwnRunActive
	}
	if len(r.active) >= r.slots {
		return errRunBusy
	}
	r.active[userID] = true
	return nil
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

// scheduleSoftDeadline is schedule.RunSoftDeadline unless a test shortened it.
func (s *server) scheduleSoftDeadline() time.Duration {
	if s.runSoftDeadline > 0 {
		return s.runSoftDeadline
	}
	return schedule.RunSoftDeadline
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
			s.pruneRunThreads(ctx, now)
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
// headless turn in a thread of its own, then the findings, the memory, the
// run record and, when there is news, a notification. A run that surely found
// nothing new leaves no thread: an answer saying so is not worth a visit.
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
	// replacing is the run's thread once it holds the prompt: however the run
	// ends, it then replaces the earlier ones the user never took up, after the
	// run is recorded. The old answer stays readable until then. Only a run
	// whose extraction found nothing new (nothingNew) drops its own thread
	// instead and leaves the earlier ones be.
	var replacing string
	var nothingNew bool
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
		// Dropped while the run still counts as running, so a message the user
		// sends meanwhile is refused (rejectWhileRunning), never deleted with it.
		if nothingNew {
			dropped, takenUp := s.dropEmptyRunThread(detached, sch.UserID, replacing)
			if dropped {
				run.MessageID = nil
			}
			if !takenUp {
				replacing = ""
			}
		}
		finish()
		if replacing != "" {
			s.replaceRunThreads(detached, sch.UserID, sch.ID, replacing)
		}
	}()

	user, found, err := s.users.FindByID(detached, sch.UserID)
	if err != nil || !found {
		run.Error = "the account is gone"
		return
	}
	thread, err := s.thread.CreateThread(detached, user.ID, chat.CreateThreadInput{
		Title: sch.Name, ScheduleID: &sch.ID,
	})
	if err != nil {
		slog.Warn("create run thread failed", "schedule_id", sch.ID, "err", err)
		run.Error = "could not open a thread for the run"
		return
	}
	// A thread the run cannot record or give its prompt would sit in the
	// recents empty, unreachable from the run: it goes again.
	dropThread := func(message string, err error) {
		slog.Warn("set up run thread failed", "schedule_id", sch.ID, "err", err)
		run.Error = message
		if _, err := s.thread.DeleteThread(detached, user.ID, thread.ID); err != nil {
			slog.Warn("delete run thread failed", "thread_id", thread.ID, "err", err)
		}
	}
	if err := s.schedules.AttachRunThread(detached, user.ID, runID, thread.ID); err != nil {
		dropThread("could not record the run's thread", err)
		return
	}
	userMessage, err := s.thread.AddMessageWithAttachments(detached, user.ID, thread.ID, chat.RoleUser, sch.Prompt, nil, nil, "")
	if err != nil {
		dropThread("could not store the prompt", err)
		return
	}
	noteThread, replacing = thread.ID, thread.ID

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
	research, reported := memory.Blocks(schedule.MemoryTokenBudget)
	turnRun := s.engine.Prepare(turn.RunConfig{
		Stream:      discardEmitter{},
		User:        user,
		Thread:      thread,
		UserMessage: userMessage,
		Usage:       usageTotal,
		Start:       start,
		Scheduled: &turn.ScheduledRun{
			TokenBudget:  schedule.RunTokenBudget,
			SoftDeadline: start.Add(s.scheduleSoftDeadline()),
			Memory:       research,
			Reported:     reported,
			Observe:      seen.add,
			Now:          s.clock,
		},
	}, turn.PrepareInput{StreamCtx: streamCtx, TurnCtx: turnCtx, ReqCtx: detached})
	result, loopErr := turnRun.RunAssistantLoop(streamCtx)
	run.Rounds = result.Rounds

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		run.Status, run.Error = schedule.StatusTimeout, "the run took longer than the time limit"
		noteThread = ""
		minutes := int(schedule.RunTimeout / time.Minute)
		// A schedule off by now (run by hand while off, or paused during the
		// run) is not turned off again; the user's own pause stays theirs.
		if current, err := s.schedules.GetSchedule(detached, user.ID, sch.ID); err != nil || !current.Enabled {
			s.persistRunNote(detached, user.ID, thread.ID, fmt.Sprintf("This scheduled run took longer than %d minutes and was stopped.", minutes))
			return
		}
		// One slow model call is no reason to turn a schedule off; a second
		// run in a row past the limit is.
		if s.previousRunTimedOut(detached, sch, runID) {
			s.disableAfterTimeout(detached, user, sch, thread)
			return
		}
		s.persistRunNote(detached, user.ID, thread.ID, fmt.Sprintf(
			"This scheduled run took longer than %d minutes and was stopped. A second run in a row past the limit turns the schedule off.", minutes))
		return
	}
	// failWith ends a run without an answer: a line in the thread says why.
	failWith := func(message, note string) {
		run.Error = message
		noteThread = ""
		s.persistRunNote(detached, user.ID, thread.ID, note)
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
		return
	}

	// The extraction gets its own deadline, and ends with the server: the
	// run's own limit is spent by now.
	extractCtx, cancelExtract := context.WithTimeout(
		llm.WithUsageAccumulator(llm.WithInferenceMetadata(ctx, inference), usageTotal), findingsTimeout)
	defer cancelExtract()
	fresh, extracted := s.recordFindings(extractCtx, user.ID, sch, runID, result, memory.Findings, seen.list())
	run.NewCount = len(fresh)
	switch {
	case result.LimitHit == turn.LimitTokens:
		run.Status = schedule.StatusTokenLimit
	case len(fresh) > 0:
		run.Status = schedule.StatusNew
	default:
		run.Status = schedule.StatusNothingNew
		// A failed extraction cannot tell: the answer may hold news.
		nothingNew = extracted
	}
	if len(fresh) == 0 {
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
// extracted is false when the extraction or storing the findings failed: no
// new item then does not mean nothing was new.
func (s *server) recordFindings(ctx context.Context, userID string, sch schedule.Schedule, runID string, result turn.LoopResult, known []schedule.Finding, seen []schedule.ToolOutcome) (fresh []schedule.Finding, extracted bool) {
	keys := make([]string, len(known))
	for i, f := range known {
		keys[i] = f.Key
	}
	items, err := s.scheduleLLM.ExtractFindings(ctx, sch.Prompt, result.Content, keys)
	extracted = err == nil
	if err != nil {
		slog.Warn("extract findings failed", "schedule_id", sch.ID, "err", err)
	}
	urls := result.SourceURLs()
	found := make([]schedule.Finding, 0, len(items))
	for _, item := range items {
		found = append(found, schedule.Finding{Key: item.Key, Label: item.Label, SourceURL: urls[item.Source]})
	}
	now := s.clock()
	fresh, err = s.schedules.AddFindings(context.WithoutCancel(ctx), userID, sch.ID, runID, found, now)
	if err != nil {
		slog.Warn("store findings failed", "schedule_id", sch.ID, "err", err)
		return nil, false
	}
	if err := s.schedules.RecordRun(context.WithoutCancel(ctx), userID, sch.ID, seen, fresh, now); err != nil {
		slog.Warn("record schedule memory failed", "schedule_id", sch.ID, "err", err)
	}
	return fresh, extracted
}

// previousRunTimedOut reports whether the run before runID also went past the
// time limit, with no edit or re-enable of the schedule since: either starts
// the count anew.
func (s *server) previousRunTimedOut(ctx context.Context, sch schedule.Schedule, runID string) bool {
	runs, err := s.schedules.ListRuns(ctx, sch.UserID, sch.ID, 2)
	if err != nil {
		slog.Warn("list schedule runs failed", "schedule_id", sch.ID, "err", err)
		return false
	}
	for _, r := range runs {
		if r.ID != runID {
			return r.Status == schedule.StatusTimeout && !r.StartedAt.Before(sch.UpdatedAt)
		}
	}
	return false
}

// disableAfterTimeout turns a schedule off whose second run in a row went past
// the time limit, says so in its thread and tells the user.
func (s *server) disableAfterTimeout(ctx context.Context, user auth.User, sch schedule.Schedule, thread chat.Thread) {
	minutes := int(schedule.RunTimeout / time.Minute)
	if err := s.schedules.Disable(ctx, user.ID, sch.ID, schedule.DisabledTimeout, s.clock()); err != nil {
		slog.Warn("disable schedule failed", "schedule_id", sch.ID, "err", err)
	}
	s.persistRunNote(ctx, user.ID, thread.ID, fmt.Sprintf("Two scheduled runs in a row took longer than %d minutes, so the schedule was disabled.", minutes))
	s.notify(ctx, user.ID, schedule.Notification{
		Kind:       schedule.KindScheduleDisabled,
		ScheduleID: &sch.ID,
		ThreadID:   &thread.ID,
		Title:      sch.Name + " was disabled",
		Body:       fmt.Sprintf("Two runs in a row took longer than %d minutes. Open the schedule to change it or enable it again.", minutes),
	})
}

// dropEmptyRunThread deletes a nothing-new run's thread unless the user took
// it up during the run (takenUp): only such a thread then replaces the earlier
// ones like any run's. Otherwise, dropped or not, the earlier run threads
// stay: the last news remains readable.
func (s *server) dropEmptyRunThread(ctx context.Context, userID, threadID string) (dropped, takenUp bool) {
	untouched, err := s.thread.IsUntouchedRunThread(ctx, userID, threadID)
	if err != nil {
		slog.Warn("check run thread failed", "thread_id", threadID, "err", err)
		return false, false
	}
	if !untouched {
		return false, true
	}
	if _, err := s.deleteThreadWithData(ctx, userID, threadID); err != nil {
		slog.Warn("drop empty run thread failed", "thread_id", threadID, "err", err)
		return false, false
	}
	return true, false
}

// replaceRunThreads deletes the schedule's run threads before keepThreadID
// that the user never took up. One they asked something in, starred, filed
// or shared stays.
func (s *server) replaceRunThreads(ctx context.Context, userID, scheduleID, keepThreadID string) {
	ids, err := s.thread.ReplaceableScheduleThreads(ctx, userID, scheduleID, keepThreadID)
	if err != nil {
		slog.Warn("list replaceable run threads failed", "schedule_id", scheduleID, "err", err)
		return
	}
	for _, id := range ids {
		if _, err := s.deleteThreadWithData(ctx, userID, id); err != nil {
			slog.Warn("replace run thread failed", "thread_id", id, "err", err)
		}
	}
}

// pruneRunThreads deletes run threads the user never took up once they are
// older than runThreadRetention.
func (s *server) pruneRunThreads(ctx context.Context, now time.Time) {
	refs, err := s.thread.StaleScheduleThreads(ctx, now.Add(-runThreadRetention), runThreadPruneBatch)
	if err != nil {
		slog.Warn("list stale run threads failed", "err", err)
		return
	}
	for _, ref := range refs {
		if _, err := s.deleteThreadWithData(ctx, ref.UserID, ref.ThreadID); err != nil {
			slog.Warn("prune run thread failed", "thread_id", ref.ThreadID, "err", err)
		}
	}
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
