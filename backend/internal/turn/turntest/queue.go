package turntest

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// MessageQueue backs ThreadStore's queued-message methods. Its own lock lets
// a test queue a message while a turn runs.
type MessageQueue struct {
	mu     sync.Mutex
	queued []chat.QueuedMessage
	seq    int
}

// Snapshot returns the queued messages.
func (q *MessageQueue) Snapshot() []chat.QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]chat.QueuedMessage(nil), q.queued...)
}

func (q *MessageQueue) take() []chat.QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.queued
	q.queued = nil
	return out
}

// EnqueueMessage implements httpapi.ThreadStore.
func (f *ThreadStore) EnqueueMessage(_ context.Context, _ string, threadID, content string, pastedTexts json.RawMessage, clientMessageID string) (chat.QueuedMessage, error) {
	f.Queue.mu.Lock()
	defer f.Queue.mu.Unlock()
	f.Queue.seq++
	if len(pastedTexts) == 0 {
		pastedTexts = json.RawMessage("[]")
	}
	m := chat.QueuedMessage{ID: "queued_" + strconv.Itoa(f.Queue.seq), ThreadID: threadID, Content: content, PastedTexts: pastedTexts, ClientMessageID: clientMessageID, CreatedAt: time.Now()}
	f.Queue.queued = append(f.Queue.queued, m)
	return m, nil
}

// ListQueued implements httpapi.ThreadStore.
func (f *ThreadStore) ListQueued(context.Context, string, string) ([]chat.QueuedMessage, error) {
	q := f.Queue.Snapshot()
	if q == nil {
		q = []chat.QueuedMessage{}
	}
	return q, nil
}

// HasQueued implements httpapi.ThreadStore.
func (f *ThreadStore) HasQueued(context.Context, string, string) (bool, error) {
	return len(f.Queue.Snapshot()) > 0, nil
}

// DeleteQueued implements httpapi.ThreadStore.
func (f *ThreadStore) DeleteQueued(_ context.Context, _ string, _ string, clientMessageID string) (bool, error) {
	f.Queue.mu.Lock()
	defer f.Queue.mu.Unlock()
	for i, m := range f.Queue.queued {
		if m.ClientMessageID == clientMessageID {
			f.Queue.queued = append(f.Queue.queued[:i], f.Queue.queued[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// DrainQueued implements httpapi.ThreadStore.
func (f *ThreadStore) DrainQueued(context.Context, string, string) ([]chat.QueuedMessage, error) {
	return f.Queue.take(), nil
}

// ConsumeQueued implements httpapi.ThreadStore: the queued messages become
// user messages.
func (f *ThreadStore) ConsumeQueued(ctx context.Context, userID, threadID string) ([]chat.Message, error) {
	var out []chat.Message
	for _, q := range f.Queue.take() {
		m, err := f.AddMessageWithAttachments(ctx, userID, threadID, chat.RoleUser, q.Content, nil, q.PastedTexts, q.ClientMessageID)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// ThreadsWithQueued implements httpapi.ThreadStore.
func (f *ThreadStore) ThreadsWithQueued(context.Context) ([]chat.QueuedThread, error) {
	if len(f.Queue.Snapshot()) == 0 {
		return nil, nil
	}
	return []chat.QueuedThread{{UserID: f.Thread.UserID, ThreadID: f.Thread.ID}}, nil
}
