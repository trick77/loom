package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
	"github.com/trick77/loom/internal/turn"
)

// Cancel causes of a turn's stream context. The handlers cancel with one of
// them and read it back via context.Cause to log why a stream ended and to
// skip work that no longer makes sense (titling a thread being deleted).
var (
	// errStreamStopRequested is the cause of an explicit client stop.
	errStreamStopRequested = errors.New("stream stop requested")
	// errStreamSuperseded is the cause when a newer request on the same
	// thread replaces the running turn.
	errStreamSuperseded = errors.New("stream superseded by newer request")
	// errStreamThreadDeleted is the cause when the turn's thread is being
	// deleted.
	errStreamThreadDeleted = errors.New("stream canceled: thread deleted")
)

// streamHeartbeatInterval is how often the SSE stream emits a keep-alive comment
// while silent. Kept well under the 60-120s idle timeouts common to proxies, load
// balancers, and edges (e.g. Cloudflare ~100s) so a long silent tool-call
// generation never trips them. See sse.Writer.Heartbeat.
const streamHeartbeatInterval = 20 * time.Second

func (s *server) handleStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	var body streamMessageRequest
	if err := decodeJSONBodyLimit(w, r, &body, maxStreamBodyBytes); err != nil {
		writeDecodeError(w, err)
		return
	}

	threadID := r.PathValue("threadID")
	thread, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeNotFound(w)
		return
	}
	priorMessages, found, err := s.thread.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "list messages failed")
		return
	}
	if !found {
		writeNotFound(w)
		return
	}
	// Resolve the attached images into model content parts BEFORE anything is
	// persisted or streamed: a bad attachment list (too many, unknown id, not an
	// image) is a plain 400 here. Once the user message is stored and the SSE
	// stream is open there is no way to reject the send cleanly. The store trims
	// content, so the text part uses the same trimmed form the message will carry.
	imageParts, imageArtifacts, err := s.engine.ResolveImageAttachments(r.Context(), user.ID, strings.TrimSpace(body.Content), body.ImageAttachmentIDs)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Persist the images and documents the user sent with this message so the sent
	// previews survive a reload (resolved user-scoped; out-of-scope ids skipped).
	sentAttachments := s.engine.ResolveSentAttachments(r.Context(), user.ID, thread, body.ImageAttachmentIDs, body.DocumentAttachmentIDs, imageArtifacts)
	// Persist the collapsed paste blocks so the sent bubble renders "Pasted" chips
	// on reload instead of the inline wall of text. Their text is already folded
	// into body.Content, so the model and every content-derived path (title,
	// classifier, RAG, history) are unchanged; this is render-only metadata.
	pastedTexts := turn.MarshalPastedTexts(body.PastedTexts)
	userMessage, err := s.thread.AddMessageWithAttachments(r.Context(), user.ID, threadID, chat.RoleUser, body.Content, sentAttachments, pastedTexts)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	// The turn runs detached from the client. A phone that freezes the tab drops
	// the connection mid-answer; the answer must still finish and be saved, so
	// the client can reattach (handleAttachStreamMessage) or find it on reload.
	// liveCtx ends only on shutdown; stop, supersede and delete cancel streamCtx
	// through activeStreams.
	liveCtx, endLive := s.detachedFromClient(r.Context())
	defer endLive()
	streamCtx, cancelStream := context.WithCancelCause(liveCtx)
	defer cancelStream(nil)
	// Sum token usage across every model call this turn makes — answer turns, tool
	// rounds, and the background reasoning/thread-title helpers — so the persisted
	// per-message stats reflect the whole turn, not just the final answer call.
	// turnStart times the full turn wall-clock for the same reason.
	usageTotal := llm.NewUsageAccumulator()
	streamCtx = llm.WithUsageAccumulator(streamCtx, usageTotal)
	// Attribute the whole turn up front, so the model calls that hang off its
	// edges — the RAG query embedding, a live vision description for an attached
	// image, the image tool — log under the same user/thread as the chat calls.
	// The per-call metadata attached below (purposes, rounds, reasoning effort)
	// builds on this; without it those edge calls logged anonymously. The
	// reasoning titles below log under it too.
	inference := llm.InferenceMetadata{
		UserID:   user.ID,
		Username: user.Username,
		ThreadID: threadID,
	}
	streamCtx = llm.WithInferenceMetadata(streamCtx, inference)
	// The prompt-assembly helpers below run on liveCtx rather than streamCtx, so
	// they need the same attribution attached separately — and the accumulator,
	// so their calls (the RAG query embedding) count in the turn.
	turnCtx := llm.WithUsageAccumulator(llm.WithInferenceMetadata(liveCtx, inference), usageTotal)
	turnStart := time.Now()
	// The turn writes its events into the hub, never to a client directly.
	stream := newTurnHub()
	unregisterStream := s.activeStreams.register(user.ID, threadID, cancelStream, stream)
	defer unregisterStream()

	writer, err := sse.NewWriter(w)
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "client_message", "sse writer init failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Keep the connection alive through idle proxies during long silent gaps —
	// notably while a model serializes a large tool-call argument server-side and
	// streams nothing to the client for up to a few minutes (see sse.Heartbeat).
	defer writer.Heartbeat(r.Context(), streamHeartbeatInterval)()
	// The sending client follows the hub like a reattaching one. The handler
	// waits for it, as w is only valid until the handler returns; sse's write
	// deadline bounds a client that stopped reading.
	following := make(chan struct{})
	go func() {
		defer close(following)
		stream.follow(r.Context(), writer)
	}()
	defer func() {
		stream.close()
		<-following
	}()
	// From here on the 200 is committed, so a panic must end the stream with an
	// error event rather than reach the recovery middleware, which can no longer
	// answer with a 500.
	defer recoverToStream(stream, r)
	// Book what the turn spent on every exit path. Deferred ahead of
	// titles.Wait below so it runs after it: the reasoning-title calls must have
	// finished before the turn's cost is read. The success path settles
	// explicitly before "done"; this is then a no-op.
	costs := s.engine.NewCostSettler(user, userMessage.ID, usageTotal)
	defer costs.Settle(context.WithoutCancel(r.Context()))
	if err := stream.SendJSON("user_message", userMessage); err != nil {
		return
	}

	// Background sweep-line generation: the working title from the question
	// right away, alongside the pre-answer gates, then one title per reasoning
	// round. The deferred waits keep any title goroutine from writing to the
	// SSE stream after the handler returns. They run before costs.Settle, so a
	// working title that outlived the answer still has its cost booked.
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
	}, turn.PrepareInput{
		StreamCtx:             streamCtx,
		TurnCtx:               turnCtx,
		ReqCtx:                liveCtx,
		ImageAttachmentIDs:    body.ImageAttachmentIDs,
		DocumentAttachmentIDs: body.DocumentAttachmentIDs,
		PriorMessages:         priorMessages,
		ImageParts:            imageParts,
	})
	// titleThread names an as-yet-untitled thread. It runs after the answer so the
	// title model can see the reply, not just the question — passing an empty
	// assistantMessage reproduces the input-only titling this handler did for
	// every turn before, and is what the paths below that never produce an answer
	// use. Detached from the request context so a canceled or failed turn still
	// names the thread, as it did when titling ran up front.
	titleThread := func(assistantMessage string) {
		if !turn.ShouldGenerateThreadTitle(thread.Title, userMessage.Content) {
			return
		}
		// The thread is being deleted, so there is nothing left to name: the title
		// call would spend tokens on a model round whose UpdateThread then no-ops on
		// the missing thread. It also runs inline on a detached context, which would
		// hold the deleting request in its bounded wait for several seconds.
		if errors.Is(context.Cause(streamCtx), errStreamThreadDeleted) {
			return
		}
		// Derived from streamCtx, not r.Context(): streamCtx carries the turn's
		// usage accumulator, so the title call's tokens land in both the
		// per-message stats and the lifetime rollup. WithoutCancel keeps that
		// value while letting the call outlive a client disconnect.
		titleCtx := context.WithoutCancel(streamCtx)
		if err := run.GenerateAndSendThreadTitle(titleCtx, assistantMessage); err != nil {
			slog.Warn("thread title generation failed", "thread_id", threadID, "error", err)
		}
	}

	// finishCosts books the turn's spend once every call has finished and tells
	// the client, ahead of the terminal event it stops reading at.
	finishCosts := func() {
		titles.Wait()
		costs.SettleAndReport(context.WithoutCancel(r.Context()), stream)
	}
	// failTurn ends a turn that has no answer to persist. The spend so far is
	// reported just before the error event (the client stops reading at it),
	// and the error goes out at once: when the upstream is down the title call
	// fails too, and waiting for it would hold the error for its whole
	// timeout. The title's own cost is booked by the deferred settle, onto
	// the message, visible from the next load.
	// It does not wait for in-flight reasoning titles either, for the same
	// reason: they go to the same upstream. Their cost, like the title's, is
	// picked up by the deferred settle.
	failTurn := func(titleSource, message string) {
		costs.SettleAndReport(context.WithoutCancel(r.Context()), stream)
		_ = stream.SendJSON("error", map[string]string{"error": message})
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
			// available; it is simply shorter than a completed answer.
			titleThread(assistantResult.Content)
			finishCosts()
			// End the stream deliberately. A client that did not issue the stop
			// itself (the thread open in a second tab) would otherwise read an
			// unterminated stream as a dropped connection.
			_ = stream.SendJSON("done", struct{}{})
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
		// question alone — exactly what every turn did before the reordering.
		failTurn(assistantContent, "empty assistant response")
		return
	}

	persistCtx := context.WithoutCancel(r.Context())
	assistantMessage, err := run.PersistAssistantTurn(persistCtx, &assistantResult)
	if err != nil {
		slog.Warn("persist assistant message failed", "thread_id", threadID, "err", err)
		failTurn(assistantContent, "persist assistant message failed")
		return
	}
	costs.SetAssistant(assistantMessage)
	if err := stream.SendJSON("assistant_message", assistantMessage); err != nil {
		return
	}

	// Name the thread now that the answer exists. This is the whole point of the
	// ordering: the reply supplies the facts, the correct spellings and a strong
	// signal of the language the turn was actually conducted in — a bare question
	// supplies none of that, and the title model used to guess from it alone.
	//
	// Deliberately after the answer is persisted and delivered, not before: this
	// call is bounded by the turn gate timeout, and a slow short-gate endpoint would
	// otherwise hold the just-streamed answer unpersisted — and the UI in its
	// streaming state — for up to that long. The cost is that the title call's
	// tokens miss the per-message stats; its cost is added onto the message and
	// its tokens reach the lifetime rollup when the turn settles below.
	titleThread(assistantContent)

	// Bump the thread to the top of the sidebar live. last_message_at was just
	// updated by the assistant message; the frontend reorders on this event
	// (ThreadShell onThread -> upsertThread). On newly-titled threads a thread event
	// was just sent by titleThread above; re-sending is idempotent (upsertThread
	// moves to top) and now carries the final last_message_at. Best-effort: a
	// failed/not-found fetch skips the event — the DB order is already correct for
	// the next refetch.
	if updated, found, getErr := s.thread.GetThread(persistCtx, user.ID, threadID); getErr == nil && found {
		_ = stream.SendJSON("thread", updated)
	}

	// Every call of the turn has finished (PersistAssistantTurn waited for the
	// reasoning titles, the thread title ran just above): book the spend and
	// report it before "done", so the open thread's Σ includes the title.
	finishCosts()

	// Best-effort, debounced background refresh of the project's shared memory so
	// sibling chats stay aware of this turn (at most once per memoryProjectDebounce).
	// Detaches from the request context so it survives the handler returning. This
	// also one-shot fills an empty project description (see refreshMemory). User
	// memory is intentionally NOT refreshed here — it is handled by the once-a-day
	// MemoryWorker sweep so it does not fire on every turn.
	s.maybeRefreshProjectMemoryAsync(r.Context(), user, thread)

	_ = stream.SendJSON("done", struct{}{})
}

func (s *server) handleStopStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	_, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeNotFound(w)
		return
	}
	// 409 when no stream is registered yet (the turn is still being set up):
	// the client then drops its fetch, which cancels that turn instead.
	if !s.activeStreams.stop(user.ID, threadID, stopCause(r.URL.Query().Get("source"))) {
		writeJSONError(w, http.StatusConflict, "no active stream")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stopCause builds the cancellation cause for an explicit client stop. It always
// wraps errStreamStopRequested (so cancel_source stays "stop_endpoint"), and folds
// the client-declared UI trigger — "stop_button", "escape", "new_send" — into the
// cause message so it surfaces in the canceled log's reason field. Attribution is
// reliable because the client awaits this stop request before aborting its fetch,
// so this cause wins the WithCancelCause race over the raw request-context cancel.
func stopCause(source string) error {
	source = sanitizeCancelSource(source)
	if source == "" {
		return errStreamStopRequested
	}
	return fmt.Errorf("%w (%s)", errStreamStopRequested, source)
}

// sanitizeCancelSource clamps a client-supplied source label to a short
// [a-z0-9_] token so it can be logged verbatim without injection risk.
func sanitizeCancelSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if len(source) > 32 {
		source = source[:32]
	}
	var b strings.Builder
	for _, r := range source {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// handleAttachStreamMessage reattaches a client to the user's running turn on
// the thread: it replays every event the turn has sent so far, then follows it
// live to the end. 204 when no turn is running; the client then reloads the
// thread, where a finished turn's answer is already saved.
func (s *server) handleAttachStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	hub := s.activeStreams.lookup(user.ID, r.PathValue("threadID"))
	if hub == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writer, err := sse.NewWriter(w)
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "client_message", "sse writer init failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer writer.Heartbeat(r.Context(), streamHeartbeatInterval)()
	hub.follow(r.Context(), writer)
}

// detachedFromClient returns a context with parent's values that a dropped
// client connection does not end; only the server's shutdown does.
func (s *server) detachedFromClient(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	stop := context.AfterFunc(s.lifetime, func() { cancel(context.Cause(s.lifetime)) })
	return ctx, func() {
		stop()
		cancel(nil)
	}
}

func streamCancelDetails(ctx context.Context) (string, string) {
	cause := context.Cause(ctx)
	if cause == nil {
		return "", ""
	}
	source := "unknown"
	switch {
	case errors.Is(cause, errStreamStopRequested):
		source = "stop_endpoint"
	case errors.Is(cause, errStreamThreadDeleted):
		source = "thread_deleted"
	case errors.Is(cause, errStreamSuperseded):
		source = "superseded_stream"
	case errors.Is(cause, context.Canceled):
		source = "request_context"
	case errors.Is(cause, context.DeadlineExceeded):
		source = "deadline"
	}
	return source, cause.Error()
}

// recoverToStream is deferred by the stream handlers once the SSE response is
// committed. It turns a panic into a terminal error event so the client sees a
// failed turn instead of a stream that just stops. The panic is logged here
// with its stack; it is not re-raised because the recovery middleware could
// only write a 500 into the open stream.
func recoverToStream(stream turn.Emitter, r *http.Request) {
	if p := recover(); p != nil {
		slog.Error("panic recovered mid-stream", "err", p, "path", r.URL.Path, "stack", string(debug.Stack()))
		_ = stream.SendJSON("error", map[string]string{"error": "internal server error"})
	}
}
