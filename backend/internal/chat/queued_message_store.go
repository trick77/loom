package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// QueuedMessage is a message the user sent while a turn was running on the
// thread, waiting to be picked up by that turn.
type QueuedMessage struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	Content  string `json:"content"`
	// PastedTexts are the collapsed paste blocks, as on Message.
	PastedTexts     json.RawMessage `json:"pastedTexts"`
	ClientMessageID string          `json:"clientMessageId"`
	CreatedAt       time.Time       `json:"createdAt"`
}

// QueuedThread names a thread that has queued messages.
type QueuedThread struct {
	UserID   string
	ThreadID string
}

const queuedColumns = "id, thread_id, content, pasted_texts, client_message_id, created_at"

// EnqueueMessage stores a message for the thread's running turn. The content
// rules are a user message's.
func (s *Store) EnqueueMessage(ctx context.Context, userID, threadID, content string, pastedTexts json.RawMessage, clientMessageID string) (QueuedMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return QueuedMessage{}, validation("message content is required")
	}
	if len(content) > maxContentLengthForRole(RoleUser) {
		return QueuedMessage{}, validation("message content is too long")
	}
	pasted, err := normalizeJSONArray("pasted texts", pastedTexts)
	if err != nil {
		return QueuedMessage{}, err
	}
	if ok, err := s.threadExists(ctx, userID, threadID); err != nil {
		return QueuedMessage{}, err
	} else if !ok {
		return QueuedMessage{}, ErrThreadNotFound
	}
	id := sqlutil.NewID()
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO queued_messages (id, thread_id, user_id, content, pasted_texts, client_message_id)
VALUES (?, ?, ?, ?, ?, ?)`,
		id, threadID, userID, content, string(pasted), clientMessageID,
	); err != nil {
		return QueuedMessage{}, fmt.Errorf("enqueue message: %w", err)
	}
	return QueuedMessage{ID: id, ThreadID: threadID, Content: content, PastedTexts: pasted, ClientMessageID: clientMessageID, CreatedAt: time.Now().UTC()}, nil
}

// ListQueued returns the thread's queued messages, oldest first.
func (s *Store) ListQueued(ctx context.Context, userID, threadID string) ([]QueuedMessage, error) {
	return listQueued(ctx, s.db, userID, threadID)
}

func listQueued(ctx context.Context, q execer, userID, threadID string) ([]QueuedMessage, error) {
	rows, err := q.QueryContext(ctx, `
SELECT `+queuedColumns+`
FROM queued_messages
WHERE user_id = ? AND thread_id = ?
ORDER BY rowid ASC`,
		userID, threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("list queued messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	queued := []QueuedMessage{}
	for rows.Next() {
		var m QueuedMessage
		var pasted, createdAt string
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Content, &pasted, &m.ClientMessageID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan queued message: %w", err)
		}
		m.PastedTexts = json.RawMessage(pasted)
		if m.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		queued = append(queued, m)
	}
	return queued, rows.Err()
}

// HasQueued reports whether the thread has queued messages.
func (s *Store) HasQueued(ctx context.Context, userID, threadID string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM queued_messages WHERE user_id = ? AND thread_id = ?`,
		userID, threadID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("count queued messages: %w", err)
	}
	return n > 0, nil
}

// DeleteQueued removes one queued message by its send id. False when it is
// gone: never queued, or already picked up.
func (s *Store) DeleteQueued(ctx context.Context, userID, threadID, clientMessageID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM queued_messages
WHERE user_id = ? AND thread_id = ? AND client_message_id = ?`,
		userID, threadID, clientMessageID,
	)
	if err != nil {
		return false, fmt.Errorf("delete queued message: %w", err)
	}
	return changed(result)
}

// DrainQueued removes and returns the thread's queued messages without
// storing them: the user stopped the turn and gets them back to edit.
func (s *Store) DrainQueued(ctx context.Context, userID, threadID string) ([]QueuedMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin drain: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queued, err := listQueued(ctx, tx, userID, threadID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM queued_messages WHERE user_id = ? AND thread_id = ?`, userID, threadID); err != nil {
		return nil, fmt.Errorf("drain queued messages: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit drain: %w", err)
	}
	return queued, nil
}

// ConsumeQueued moves the thread's queued messages into the transcript as
// user messages, in one transaction, and returns the stored messages.
func (s *Store) ConsumeQueued(ctx context.Context, userID, threadID string) ([]Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin consume: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queued, err := listQueued(ctx, tx, userID, threadID)
	if err != nil || len(queued) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(queued))
	for _, q := range queued {
		id, err := insertMessageTx(ctx, tx, messageInsert{
			userID:          userID,
			threadID:        threadID,
			role:            RoleUser,
			content:         q.Content,
			pastedTexts:     q.PastedTexts,
			clientMessageID: q.ClientMessageID,
		})
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM queued_messages WHERE user_id = ? AND id = ?`, userID, q.ID); err != nil {
			return nil, fmt.Errorf("delete consumed message: %w", err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit consume: %w", err)
	}
	messages := make([]Message, 0, len(ids))
	for _, id := range ids {
		m, ok, err := s.getMessage(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("consumed message %s not found", id)
		}
		messages = append(messages, m)
	}
	return messages, nil
}

// ThreadsWithQueued lists every thread, of any user, that has queued
// messages. The boot pass uses it to pick up messages a restart left behind.
func (s *Store) ThreadsWithQueued(ctx context.Context) ([]QueuedThread, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT user_id, thread_id FROM queued_messages`)
	if err != nil {
		return nil, fmt.Errorf("list threads with queued messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var threads []QueuedThread
	for rows.Next() {
		var q QueuedThread
		if err := rows.Scan(&q.UserID, &q.ThreadID); err != nil {
			return nil, fmt.Errorf("scan queued thread: %w", err)
		}
		threads = append(threads, q)
	}
	return threads, rows.Err()
}
