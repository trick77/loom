package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/turn"
	"github.com/trick77/ship/kit/httpx"
)

// queueMessageRequest is a message sent while a turn runs. Queued messages
// carry text and pasted blocks only: attachments shape the turn's whole
// prompt, which a turn already under way cannot take in.
type queueMessageRequest struct {
	Content         string                   `json:"content"`
	PastedTexts     []chat.MessagePastedText `json:"pastedTexts"`
	ClientMessageID string                   `json:"clientMessageId"`
}

// handleQueueMessage queues a message behind the thread's running turn, which
// picks it up at its next tool boundary or answers it in a follow-up turn.
// With no turn running (or the running one past its last look at the queue)
// it starts one for it. 202 either way; the client follows the turn through
// its own stream or messages:attach.
func (s *server) handleQueueMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	if s.llm == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	var body queueMessageRequest
	if err := decodeJSONBodyLimit(w, r, &body, maxStreamBodyBytes); err != nil {
		writeDecodeError(w, err)
		return
	}
	// The send id is how the client matches the message once a turn picks it
	// up, and how it takes it back: required here.
	if body.ClientMessageID == "" || !validSendID(body.ClientMessageID) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid clientMessageId")
		return
	}
	threadID := r.PathValue("threadID")
	if _, found, err := s.thread.GetThread(r.Context(), user.ID, threadID); err != nil {
		serverError(w, r, err, "get thread failed")
		return
	} else if !found {
		writeNotFound(w)
		return
	}
	liveCtx, endLive := s.detachedFromClient(r.Context())
	streamCtx, cancelStream := context.WithCancelCause(liveCtx)
	hub := newTurnHub()
	insert := func() error {
		_, err := s.thread.EnqueueMessage(r.Context(), user.ID, threadID, body.Content, turn.MarshalPastedTexts(body.PastedTexts), body.ClientMessageID)
		return err
	}
	entry, unregister, err := s.activeStreams.enqueue(user.ID, threadID, insert, cancelStream, hub)
	if err != nil {
		cancelStream(nil)
		endLive()
		writeStoreError(w, r, err)
		return
	}
	if entry == nil {
		cancelStream(nil)
		endLive()
	} else {
		s.startTurnChain(&turnChain{s: s, user: user, threadID: threadID, stream: entry, hub: hub, liveCtx: liveCtx, streamCtx: streamCtx}, nil, cancelStream, unregister, endLive)
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleDeleteQueuedMessage takes a queued message back. 404 when it is gone,
// which includes a turn having just picked it up: the client then sees it
// arrive as a user_message.
func (s *server) handleDeleteQueuedMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	deleted, err := s.thread.DeleteQueued(r.Context(), user.ID, r.PathValue("threadID"), r.PathValue("clientMessageID"))
	if err != nil {
		serverError(w, r, err, "delete queued message failed")
		return
	}
	if !deleted {
		writeNotFound(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resumeQueuedTurns answers the messages a restart left queued: their turn
// died with the process, so each such thread gets a turn of its own. It
// returns once the turns are started.
func (s *server) resumeQueuedTurns(ctx context.Context) {
	if s.thread == nil || s.users == nil || s.llm == nil {
		return
	}
	threads, err := s.thread.ThreadsWithQueued(ctx)
	if err != nil {
		slog.Warn("list queued threads failed", "err", err)
		return
	}
	for _, q := range threads {
		user, found, err := s.users.FindByID(ctx, q.UserID)
		if err != nil || !found {
			slog.Warn("resume queued turn: user not found", "user_id", q.UserID, "thread_id", q.ThreadID, "err", err)
			continue
		}
		s.startQueuedChain(ctx, user, q.ThreadID)
	}
}

// startQueuedChain starts a stream that answers the thread's queue, unless a
// running one will.
func (s *server) startQueuedChain(ctx context.Context, user auth.User, threadID string) {
	liveCtx, endLive := s.detachedFromClient(ctx)
	streamCtx, cancelStream := context.WithCancelCause(liveCtx)
	hub := newTurnHub()
	entry, unregister, _ := s.activeStreams.enqueue(user.ID, threadID, func() error { return nil }, cancelStream, hub)
	if entry == nil {
		cancelStream(nil)
		endLive()
		return
	}
	slog.Info("resuming queued messages", "thread_id", threadID)
	s.startTurnChain(&turnChain{s: s, user: user, threadID: threadID, stream: entry, hub: hub, liveCtx: liveCtx, streamCtx: streamCtx}, nil, cancelStream, unregister, endLive)
}
