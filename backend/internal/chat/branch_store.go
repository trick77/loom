package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// ErrNotAnAnswer is a branch from a message that is not an assistant answer.
var ErrNotAnAnswer = errors.New("only an answer can be continued in a new thread")

// copiedMessageColumns are the message columns a branch copies; the ids,
// timestamps and the spend stay with the original.
const copiedMessageColumns = "role, content, reasoning_content, tool_calls, citations, artifacts, attachments, pasted_texts, activity_trace, content_blocks, model, reasoning_effort"

// BranchThread starts a new thread from one of the user's answers: a copy of
// the answer and the question it answered, so the thread continues from them
// on its own. The copy keeps no spend (it was booked on the original) and the
// new thread names its source. The thread survives the source's deletion.
func (s *Store) BranchThread(ctx context.Context, userID, threadID, messageID string) (Thread, error) {
	var role Role
	var rowid int64
	err := s.db.QueryRowContext(ctx, `SELECT role, rowid FROM messages WHERE user_id = ? AND thread_id = ? AND id = ?`,
		userID, threadID, messageID).Scan(&role, &rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return Thread{}, ErrThreadNotFound
	}
	if err != nil {
		return Thread{}, fmt.Errorf("find branch source: %w", err)
	}
	if role != RoleAssistant {
		return Thread{}, ErrNotAnAnswer
	}
	copies := []string{messageID}
	var questionID string
	err = s.db.QueryRowContext(ctx, `
SELECT id FROM messages WHERE user_id = ? AND thread_id = ? AND role = 'user' AND rowid < ?
ORDER BY rowid DESC LIMIT 1`, userID, threadID, rowid).Scan(&questionID)
	switch {
	case err == nil:
		copies = []string{questionID, messageID}
	case !errors.Is(err, sql.ErrNoRows):
		return Thread{}, fmt.Errorf("find branch question: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Thread{}, fmt.Errorf("begin branch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	newID := sqlutil.NewID()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title, branched_from_thread_id, branched_from_message_id, last_message_at)
VALUES (?, ?, ?, ?, ?, datetime('now'))`, newID, userID, DefaultThreadTitle, threadID, messageID); err != nil {
		return Thread{}, fmt.Errorf("insert branch thread: %w", err)
	}
	for _, id := range copies {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO messages (id, thread_id, user_id, `+copiedMessageColumns+`)
SELECT ?, ?, user_id, `+copiedMessageColumns+` FROM messages WHERE user_id = ? AND id = ?`,
			sqlutil.NewID(), newID, userID, id); err != nil {
			return Thread{}, fmt.Errorf("copy branch message: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Thread{}, fmt.Errorf("commit branch: %w", err)
	}
	thread, ok, err := s.GetThread(ctx, userID, newID)
	if err != nil {
		return Thread{}, err
	}
	if !ok {
		return Thread{}, errors.New("branch thread not found")
	}
	return thread, nil
}

// SetLastMessageAt puts a thread's activity time back to at (nil clears it):
// a scheduled run that found nothing new keeps its thread where it was in the
// recents.
func (s *Store) SetLastMessageAt(ctx context.Context, userID, threadID string, at *time.Time) error {
	var value any
	if at != nil {
		value = at.UTC().Format(time.DateTime)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE threads SET last_message_at = ? WHERE user_id = ? AND id = ?`, value, userID, threadID); err != nil {
		return fmt.Errorf("set last message time: %w", err)
	}
	return nil
}
