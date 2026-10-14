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

	if !validSendID(body.ClientMessageID) {
		writeJSONError(w, http.StatusBadRequest, "invalid clientMessageId")
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
	userMessage, err := s.thread.AddMessageWithAttachments(r.Context(), user.ID, threadID, chat.RoleUser, body.Content, sentAttachments, pastedTexts, body.ClientMessageID)
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
	streamCtx, cancelStream := context.WithCancelCause(liveCtx)
	// The turn writes its events into the hub, never to a client directly.
	hub := newTurnHub()
	entry, unregister := s.activeStreams.registerStream(user.ID, threadID, body.ClientMessageID, cancelStream, hub)
	writer, err := sse.NewWriter(w)
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "client_message", "sse writer init failed", "err", err)
		cancelStream(nil)
		unregister()
		hub.close()
		endLive()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = hub.SendJSON("user_message", userMessage)
	chain := &turnChain{s: s, user: user, threadID: threadID, stream: entry, hub: hub, liveCtx: liveCtx, streamCtx: streamCtx}
	finished := s.startTurnChain(chain, &turnInput{
		thread:      thread,
		userMessage: userMessage,
		prior:       priorMessages,
		imageIDs:    body.ImageAttachmentIDs,
		documentIDs: body.DocumentAttachmentIDs,
		imageParts:  imageParts,
	}, cancelStream, unregister, endLive)
	// Keep the connection alive through idle proxies during long silent gaps —
	// notably while a model serializes a large tool-call argument server-side and
	// streams nothing to the client for up to a few minutes (see sse.Heartbeat).
	defer writer.Heartbeat(r.Context(), streamHeartbeatInterval)()
	// The sending client follows the hub like a reattaching one, until the
	// chain ends the stream or the client goes away. The handler then waits
	// for the chain, so a send's request is open as long as its turns run:
	// the server's graceful shutdown waits for them as before.
	hub.follow(r.Context(), writer, false)
	<-finished
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
	// 409 when the send's turn is not registered yet (it is still being set
	// up). The turn runs detached from the client, so the client dropping its
	// fetch does not end it: a stop naming the send (sendId) is kept and
	// applied when that turn registers.
	sendID := r.URL.Query().Get("sendId")
	if !validSendID(sendID) {
		writeJSONError(w, http.StatusBadRequest, "invalid sendId")
		return
	}
	if !s.activeStreams.stop(user.ID, threadID, sendID, stopCause(r.URL.Query().Get("source"))) {
		writeJSONError(w, http.StatusConflict, "no active stream")
		return
	}
	// Stopping hands the messages queued behind the turn back to the user,
	// who wanted control back, instead of answering them. The stream was
	// marked finishing first, and the drain shares the queue lock with every
	// consume, so a message is either picked up before the stop or handed
	// back, never lost in between. What is queued after the drain is answered
	// once the stopped turn has ended (see handOff).
	var queued []chat.QueuedMessage
	s.activeStreams.withQueueLock(user.ID, threadID, func() {
		queued, err = s.thread.DrainQueued(context.WithoutCancel(r.Context()), user.ID, threadID)
	})
	if err != nil {
		slog.Warn("drain queued messages failed", "thread_id", threadID, "err", err)
	}
	if len(queued) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, stopStreamResponse{Queued: queued})
}

// stopStreamResponse carries the queued messages a stop handed back.
type stopStreamResponse struct {
	Queued []chat.QueuedMessage `json:"queued"`
}

// validSendID accepts "" (no id) or a client send id: up to 64 characters of
// [A-Za-z0-9_-]. It is stored and used as a map key, never interpreted.
func validSendID(id string) bool {
	if len(id) > 64 {
		return false
	}
	for _, c := range id {
		allowed := c == '-' || c == '_' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
		if !allowed {
			return false
		}
	}
	return true
}

// stopCause builds the cancellation cause for an explicit client stop. It always
// wraps errStreamStopRequested (so cancel_source stays "stop_endpoint"), and folds
// the client-declared UI trigger — "stop_button", "escape", "new_send" — into the
// cause message so it surfaces in the canceled log's reason field.
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
	hub.follow(r.Context(), writer, true)
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
