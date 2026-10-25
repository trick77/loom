package chat

import (
	"context"
	"fmt"
	"time"
)

// untouchedRunThread matches a scheduled run's thread the user never took up:
// no question after the run's prompt, nothing queued, not starred, archived,
// filed into a project or shared, and no unread notification pointing at it
// (its news would vanish unseen). Such a thread is replaced by the schedule's
// next run and purged once old; every other run thread is the user's to keep.
const untouchedRunThread = `t.schedule_id IS NOT NULL
  AND t.starred = 0
  AND t.archived_at IS NULL
  AND t.project_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.user_id = t.user_id AND n.thread_id = t.id AND n.read_at IS NULL)
  AND (SELECT COUNT(*) FROM messages m WHERE m.thread_id = t.id AND m.role = 'user') <= 1
  AND NOT EXISTS (SELECT 1 FROM queued_messages q WHERE q.user_id = t.user_id AND q.thread_id = t.id)
  AND NOT EXISTS (SELECT 1 FROM shared_threads sh WHERE sh.user_id = t.user_id AND sh.thread_id = t.id AND sh.shared = 1)`

// ThreadRef names one user's thread.
type ThreadRef struct {
	UserID   string
	ThreadID string
}

// ReplaceableScheduleThreads lists the schedule's untouched run threads other
// than keepThreadID, the run that replaces them.
func (s *Store) ReplaceableScheduleThreads(ctx context.Context, userID, scheduleID, keepThreadID string) ([]string, error) {
	refs, err := s.untouchedRunThreads(ctx, `t.user_id = ? AND t.schedule_id = ? AND t.id != ?`, userID, scheduleID, keepThreadID)
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = ref.ThreadID
	}
	return ids, err
}

// StaleScheduleThreads lists up to limit untouched run threads, across all
// users, created before the cutoff, oldest first. An enabled schedule's newest
// run thread stays: a schedule that runs less than monthly keeps its answer
// until the next run replaces it.
func (s *Store) StaleScheduleThreads(ctx context.Context, before time.Time, limit int) ([]ThreadRef, error) {
	return s.untouchedRunThreads(ctx, `t.created_at < ?
  AND NOT (
    EXISTS (SELECT 1 FROM schedules sch WHERE sch.id = t.schedule_id AND sch.enabled = 1)
    AND NOT EXISTS (SELECT 1 FROM threads n WHERE n.schedule_id = t.schedule_id AND n.created_at > t.created_at))
ORDER BY t.created_at LIMIT ?`, before.UTC().Format(time.DateTime), limit)
}

// untouchedRunThreads lists the untouched run threads that also match where,
// which may end in ORDER BY and LIMIT.
func (s *Store) untouchedRunThreads(ctx context.Context, where string, args ...any) ([]ThreadRef, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.user_id, t.id FROM threads t
WHERE `+untouchedRunThread+` AND `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list untouched run threads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var refs []ThreadRef
	for rows.Next() {
		var ref ThreadRef
		if err := rows.Scan(&ref.UserID, &ref.ThreadID); err != nil {
			return nil, fmt.Errorf("scan untouched run thread: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list untouched run threads: %w", err)
	}
	return refs, nil
}
