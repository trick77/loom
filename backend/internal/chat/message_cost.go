package chat

import (
	"context"
	"fmt"
)

// AddMessageCost adds nanoUSD to a message's cost. It is for spend that
// belongs to a turn but lands after (or without) the assistant message being
// written: the thread title, or every call of a turn that failed before it had
// an answer to persist. The thread's Σ is the sum of its messages' costs, so
// this is what keeps such calls in it. Scoped by user_id; false when the
// message is not the user's.
func (s *Store) AddMessageCost(ctx context.Context, userID, messageID string, nanoUSD int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE messages SET cost_nano_usd = COALESCE(cost_nano_usd, 0) + ?
WHERE user_id = ? AND id = ?`,
		nanoUSD, userID, messageID,
	)
	if err != nil {
		return false, fmt.Errorf("add message cost: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("add message cost: %w", err)
	}
	return n > 0, nil
}
