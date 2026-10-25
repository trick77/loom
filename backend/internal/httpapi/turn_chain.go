package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/turn"
)

// turnChain drives one registered stream on a thread: the turn a send
// started, then a follow-up turn for each batch of messages the user queued
// meanwhile that no tool boundary picked up. Every turn writes into the same
// hub, so a client following it sees one stream that ends with a single done
// (or error) after the last turn.
type turnChain struct {
	s        *server
	user     auth.User
	threadID string
	stream   *activeStream
	hub      *turnHub
	// liveCtx ends only on shutdown; streamCtx also on stop, supersede and
	// delete (see activeStreams).
	liveCtx   context.Context
	streamCtx context.Context
	// finished is set once the terminal event went out.
	finished bool
	// noHandOff keeps the end from starting a stream for the queue: the
	// queue could not be read.
	noHandOff bool
}

// turnInput is one turn's question and what the model gets with it.
type turnInput struct {
	thread      chat.Thread
	userMessage chat.Message
	prior       []chat.Message
	imageIDs    []string
	documentIDs []string
	imageParts  []llm.MessageContentPart
}

// startTurnChain registers nothing itself: the caller registered stream with
// cancel. It runs the chain detached from any request, first answering first
// (nil: start from the queue), and ends the stream when the chain is done.
// The returned channel closes then.
func (s *server) startTurnChain(c *turnChain, first *turnInput, cancel context.CancelCauseFunc, unregister, endLive func()) <-chan struct{} {
	finished := make(chan struct{})
	end := func() {
		cancel(nil)
		unregister()
		c.hub.close()
		endLive()
		close(finished)
	}
	if !s.background.Spawn(c.liveCtx, "turn:"+c.threadID, func(groupCtx context.Context) {
		defer end()
		// The background group giving up on its tasks at shutdown ends the
		// turn too, so it does not write past the database's close.
		stopWithGroup := context.AfterFunc(groupCtx, func() { cancel(context.Cause(groupCtx)) })
		defer stopWithGroup()
		c.run(first)
	}) {
		// Shutting down: the queued messages wait for the next boot. Not
		// through endWith: this may run inside a hand-off, which holds the
		// thread's queue lock.
		s.activeStreams.abandon(c.stream)
		c.finished = true
		_ = c.hub.SendJSON("done", struct{}{})
		end()
	}
	return finished
}

// startHandOffChain starts the stream that answers what a finished stream
// handed off. Called by handOff, under the thread's queue lock.
func (s *server) startHandOffChain(parent context.Context, user auth.User, threadID string) {
	liveCtx, endLive := s.detachedFromClient(parent)
	streamCtx, cancelStream := context.WithCancelCause(liveCtx)
	hub := newTurnHub()
	entry, unregister := s.activeStreams.registerHandOff(user.ID, threadID, cancelStream, hub)
	s.startTurnChain(&turnChain{s: s, user: user, threadID: threadID, stream: entry, hub: hub, liveCtx: liveCtx, streamCtx: streamCtx}, nil, cancelStream, unregister, endLive)
}

// run answers first, then whatever was queued, until a look at the queue
// finds it empty or the stream was stopped.
func (c *turnChain) run(first *turnInput) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("panic recovered mid-stream", "err", p, "thread_id", c.threadID, "stack", string(debug.Stack()))
			c.s.activeStreams.finishIfIdle(c.stream, true, nil)
			c.endWith("error", map[string]string{"error": "internal server error"})
		}
	}()
	in := first
	for {
		if in != nil {
			c.runTurn(*in)
			if c.finished {
				return
			}
		}
		if c.finish() {
			c.endWith("done", struct{}{})
			return
		}
		next, err := c.nextFromQueue()
		if err != nil {
			slog.Warn("start follow-up turn failed", "thread_id", c.threadID, "err", err)
			c.returnQueue()
			c.endWith("done", struct{}{})
			return
		}
		// nil: the queue emptied (or was stopped) meanwhile; the next look
		// ends the chain.
		in = next
	}
}

// endWith ends the stream: what was queued since the last look goes to a
// stream of its own, then the terminal event goes out. The hand-off comes
// first, so a client that attaches once it reads the event finds that stream.
func (c *turnChain) endWith(event string, data any) {
	if c.finished {
		return
	}
	c.finished = true
	hasQueued := c.hasQueued
	if c.noHandOff {
		hasQueued = func() bool { return false }
	}
	c.s.activeStreams.handOff(c.stream, hasQueued, func() {
		c.s.startHandOffChain(c.liveCtx, c.user, c.threadID)
	})
	_ = c.hub.SendJSON(event, data)
}

// returnQueue hands the queued messages back to the client when the chain
// cannot answer them, rather than leaving them for a later, unrelated turn.
// When even that fails they stay queued, for the next boot.
func (c *turnChain) returnQueue() {
	c.s.activeStreams.finishIfIdle(c.stream, true, nil)
	var queued []chat.QueuedMessage
	var err error
	c.s.activeStreams.withQueueLock(c.user.ID, c.threadID, func() {
		queued, err = c.s.thread.DrainQueued(context.WithoutCancel(c.liveCtx), c.user.ID, c.threadID)
	})
	if err != nil {
		slog.Warn("return queued messages failed", "thread_id", c.threadID, "err", err)
		c.noHandOff = true
		return
	}
	if len(queued) > 0 {
		_ = c.hub.SendJSON("queue_returned", stopStreamResponse{Queued: queued})
	}
}

func (c *turnChain) hasQueued() bool {
	has, err := c.s.thread.HasQueued(context.WithoutCancel(c.liveCtx), c.user.ID, c.threadID)
	if err != nil {
		slog.Warn("check queued messages failed", "thread_id", c.threadID, "err", err)
	}
	return has
}

// finish is the chain's look at the queue between turns; see finishIfIdle.
func (c *turnChain) finish() bool {
	return c.s.activeStreams.finishIfIdle(c.stream, c.streamCtx.Err() != nil, c.hasQueued)
}

// fail reports a turn that has no answer. With more queued, the chain goes
// on and the failure is reported without ending the stream.
func (c *turnChain) fail(message string) {
	if c.finish() {
		c.endWith("error", map[string]string{"error": message})
		return
	}
	_ = c.hub.SendJSON("turn_error", map[string]string{"error": message})
}

// nextFromQueue moves the queued messages into the thread and returns the
// follow-up turn answering them; nil when the queue emptied meanwhile.
func (c *turnChain) nextFromQueue() (*turnInput, error) {
	ctx := context.WithoutCancel(c.liveCtx)
	thread, found, err := c.s.thread.GetThread(ctx, c.user.ID, c.threadID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("thread not found")
	}
	prior, _, err := c.s.thread.ListMessages(ctx, c.user.ID, c.threadID)
	if err != nil {
		return nil, err
	}
	consumed, err := c.s.activeStreams.consume(c.stream, func() ([]chat.Message, error) {
		return c.s.thread.ConsumeQueued(ctx, c.user.ID, c.threadID)
	})
	if err != nil || len(consumed) == 0 {
		return nil, err
	}
	// The turns before this one are stored: a client reattaching from here
	// on loads them with the thread and replays only this one.
	c.hub.startReplayHere()
	for _, m := range consumed {
		_ = c.hub.SendJSON("user_message", m)
	}
	last := len(consumed) - 1
	return &turnInput{
		thread:      thread,
		userMessage: consumed[last],
		prior:       append(prior, consumed[:last]...),
	}, nil
}

// queueInbox lets a running turn pick up the thread's queued messages at a
// tool boundary. A stopped stream picks up nothing: stop hands them back.
type queueInbox struct{ c *turnChain }

func (q queueInbox) HasQueued(ctx context.Context) (bool, error) {
	if q.c.s.activeStreams.isFinishing(q.c.stream) {
		return false, nil
	}
	return q.c.s.thread.HasQueued(ctx, q.c.user.ID, q.c.threadID)
}

func (q queueInbox) Consume(ctx context.Context) ([]chat.Message, error) {
	return q.c.s.activeStreams.consume(q.c.stream, func() ([]chat.Message, error) {
		return q.c.s.thread.ConsumeQueued(ctx, q.c.user.ID, q.c.threadID)
	})
}

// runTurn answers one question into the hub, up to and including its
// assistant_message (or its failure); the terminal event is the chain's.
func (c *turnChain) runTurn(in turnInput) {
	s, user, threadID, thread, userMessage := c.s, c.user, c.threadID, in.thread, in.userMessage
	stream := c.hub
	detached := context.WithoutCancel(c.liveCtx)
	// Sum token usage across every model call this turn makes — answer turns, tool
	// rounds, and the background reasoning/thread-title helpers — so the persisted
	// per-message stats reflect the whole turn, not just the final answer call.
	// turnStart times the full turn wall-clock for the same reason.
	usageTotal := llm.NewUsageAccumulator()
	// Attribute the whole turn up front, so the model calls that hang off its
	// edges — the RAG query embedding, a live vision description for an attached
	// image, the image tool — log under the same user/thread as the chat calls.
	inference := llm.InferenceMetadata{
		UserID:   user.ID,
		Username: user.Username,
		ThreadID: threadID,
	}
	streamCtx := llm.WithInferenceMetadata(llm.WithUsageAccumulator(c.streamCtx, usageTotal), inference)
	// The prompt-assembly helpers run on liveCtx rather than streamCtx, so they
	// need the same attribution attached separately — and the accumulator, so
	// their calls (the RAG query embedding) count in the turn.
	turnCtx := llm.WithUsageAccumulator(llm.WithInferenceMetadata(c.liveCtx, inference), usageTotal)
	turnStart := time.Now()
	// Book what the turn spent on every exit path. Deferred ahead of
	// titles.Wait below so it runs after it: the reasoning-title calls must have
	// finished before the turn's cost is read. The success path settles
	// explicitly before the terminal event; this is then a no-op.
	costs := s.engine.NewCostSettler(user, userMessage.ID, usageTotal)
	defer costs.Settle(detached)

	// Background sweep-line generation: the working title from the question
	// right away, alongside the pre-answer gates, then one title per reasoning
	// round. The deferred waits keep any title goroutine from writing to the
	// stream after the turn ends. They run before costs.Settle, so a working
	// title that outlived the answer still has its cost booked.
	titles := turn.NewReasoningTitleTracker(streamCtx, s.llm, stream, inference, user.ResponseLanguageName())
	defer titles.Wait()
	defer titles.WaitWorking()
	titles.SpawnWorking(userMessage.Content)

	run := s.engine.Prepare(turn.RunConfig{
		Stream:      stream,
		Titles:      titles,
		User:        user,
		Thread:      thread,
		UserMessage: userMessage,
		Usage:       usageTotal,
		Start:       turnStart,
		Inbox:       queueInbox{c: c},
		Timezone:    s.timezoneFor(user.ID),
	}, turn.PrepareInput{
		StreamCtx:             streamCtx,
		TurnCtx:               turnCtx,
		ReqCtx:                c.liveCtx,
		ImageAttachmentIDs:    in.imageIDs,
		DocumentAttachmentIDs: in.documentIDs,
		PriorMessages:         in.prior,
		ImageParts:            in.imageParts,
	})
	// titleThread names an as-yet-untitled thread. It runs after the answer so the
	// title model can see the reply, not just the question — passing an empty
	// assistantMessage reproduces input-only titling, which the paths below that
	// never produce an answer use. Detached so a canceled or failed turn still
	// names the thread.
	titleThread := func(assistantMessage string) {
		// A scheduled run's thread carries its schedule's name, which a
		// follow-up may match by chance.
		if thread.ScheduleID != nil || !turn.ShouldGenerateThreadTitle(thread.Title, userMessage.Content) {
			return
		}
		// The thread is being deleted, so there is nothing left to name: the title
		// call would spend tokens on a model round whose UpdateThread then no-ops on
		// the missing thread, and hold the deleting request in its bounded wait.
		if errors.Is(context.Cause(streamCtx), errStreamThreadDeleted) {
			return
		}
		// Derived from streamCtx: it carries the turn's usage accumulator, so the
		// title call's tokens land in both the per-message stats and the lifetime
		// rollup. WithoutCancel keeps that value while letting the call outlive a
		// stop.
		titleCtx := context.WithoutCancel(streamCtx)
		if err := run.GenerateAndSendThreadTitle(titleCtx, assistantMessage); err != nil {
			slog.Warn("thread title generation failed", "thread_id", threadID, "error", err)
		}
	}

	// finishCosts books the turn's spend once every call has finished and tells
	// the client, ahead of the terminal event it stops reading at.
	finishCosts := func() {
		titles.Wait()
		costs.SettleAndReport(detached, stream)
	}
	// failTurn ends a turn that has no answer to persist. The spend so far is
	// reported just before the error event (the client stops reading at it),
	// and the error goes out at once: when the upstream is down the title call
	// fails too, and waiting for it would hold the error for its whole timeout.
	// It does not wait for in-flight reasoning titles either, for the same
	// reason; their cost, like the title's, is picked up by the deferred settle.
	failTurn := func(titleSource, message string) {
		costs.SettleAndReport(detached, stream)
		c.fail(message)
		titleThread(titleSource)
	}

	assistantResult, err := run.RunAssistantLoop(streamCtx)
	if err != nil {
		if turn.StreamCanceled(streamCtx, err) {
			cancelSource, cancelReason := streamCancelDetails(streamCtx)
			slog.Info("message stream canceled",
				"thread_id", threadID,
				"cancel_source", cancelSource,
				"reason", cancelReason,
				"err", err,
				"content_bytes", len(assistantResult.Content),
				"reasoning_bytes", len(assistantResult.ReasoningContent),
				"tool_calls", len(assistantResult.ToolCalls))
			// Whatever streamed before the cancel is still the best title source
			// available; it is simply shorter than a completed answer. The chain
			// then ends the stream with done, deliberately: a client that did
			// not issue the stop itself (the thread open in a second tab) would
			// otherwise read an unterminated stream as a dropped connection.
			titleThread(assistantResult.Content)
			finishCosts()
			return
		}
		failTurn(assistantResult.Content, turn.StreamFailureMessage(err, assistantResult, "message", threadID))
		return
	}
	assistantContent := assistantResult.Content
	if run.ImageRequired() && len(assistantResult.Artifacts) == 0 {
		message := "image generation was not completed"
		if strings.TrimSpace(assistantResult.ToolError) != "" {
			message = assistantResult.ToolError
		}
		slog.Warn("image request completed without image artifact",
			"thread_id", threadID,
			"content_bytes", len(assistantResult.Content),
			"tool_calls", len(assistantResult.ToolCalls),
			"tool_error", assistantResult.ToolError)
		failTurn(assistantContent, message)
		return
	}
	if strings.TrimSpace(assistantContent) == "" {
		slog.Warn("empty assistant response",
			"thread_id", threadID,
			"content_bytes", len(assistantResult.Content),
			"reasoning_bytes", len(assistantResult.ReasoningContent),
			"tool_calls", len(assistantResult.ToolCalls))
		// assistantContent is empty here by definition, so this titles from the
		// question alone.
		failTurn(assistantContent, "empty assistant response")
		return
	}

	assistantMessage, err := run.PersistAssistantTurn(detached, &assistantResult)
	if err != nil {
		slog.Warn("persist assistant message failed", "thread_id", threadID, "err", err)
		failTurn(assistantContent, "persist assistant message failed")
		return
	}
	costs.SetAssistant(assistantMessage)
	if err := stream.SendJSON("assistant_message", assistantMessage); err != nil {
		return
	}

	// Name the thread now that the answer exists: the reply supplies the facts,
	// the correct spellings and the language the turn was conducted in.
	// Deliberately after the answer is persisted and delivered, so a slow title
	// endpoint never holds the just-streamed answer unpersisted. The title call's
	// tokens miss the per-message stats; its cost is added onto the message and
	// its tokens reach the lifetime rollup when the turn settles below.
	titleThread(assistantContent)

	// Bump the thread to the top of the sidebar live. last_message_at was just
	// updated by the assistant message; the frontend reorders on this event.
	// Best-effort: a failed fetch skips the event — the DB order is already
	// correct for the next refetch.
	if updated, found, getErr := s.thread.GetThread(detached, user.ID, threadID); getErr == nil && found {
		_ = stream.SendJSON("thread", updated)
	}

	// Every call of the turn has finished: book the spend and report it before
	// the terminal event, so the open thread's Σ includes the title.
	finishCosts()

	// Best-effort, debounced background refresh of the project's shared memory so
	// sibling chats stay aware of this turn (at most once per memoryProjectDebounce).
	// User memory is refreshed by the once-a-day MemoryWorker sweep instead.
	s.maybeRefreshProjectMemoryAsync(c.liveCtx, user, thread)
}
