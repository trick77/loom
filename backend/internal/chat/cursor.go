package chat

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// sqliteTimeLayout is the textual datetime format SQLite stores (and orders)
// timestamps in. Rendering cursor bounds with this exact layout keeps the
// keyset comparison aligned with the lexical ORDER BY on these columns.
//
// Invariant: created_at/updated_at/last_message_at are written via
// datetime('now') (schema defaults / store writes), i.e. always UTC and in
// exactly this layout — no fractional seconds, 'T'/'Z', or timezone offset.
// The keyset bound is compared lexically against the raw column text, so any
// row stored in a different format would silently shift the page boundary
// (skipped/duplicated rows). The existing ORDER BY relies on the same lexical
// invariant, so this does not add a new assumption.
const sqliteTimeLayout = "2006-01-02 15:04:05"

const (
	defaultThreadLimit = 30
	maxThreadLimit     = 1000
)

// threadCursorPayload is the keyset position of the last returned thread,
// mirroring the ORDER BY tuple (COALESCE(last_message_at, updated_at),
// updated_at, id).
type threadCursorPayload struct {
	Activity string `json:"a"`
	Updated  string `json:"u"`
	ID       string `json:"id"`
}

// EncodeThreadCursor builds the opaque cursor pointing just past the given
// thread, for fetching the next page in the same ORDER BY.
func EncodeThreadCursor(t Thread) string {
	activity := t.UpdatedAt
	if t.LastMessageAt != nil {
		activity = *t.LastMessageAt
	}
	payload := threadCursorPayload{
		Activity: activity.UTC().Format(sqliteTimeLayout),
		Updated:  t.UpdatedAt.UTC().Format(sqliteTimeLayout),
		ID:       t.ID,
	}
	raw, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeThreadCursor(encoded string) (threadCursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return threadCursorPayload{}, fmt.Errorf("decode cursor: %w", err)
	}
	var payload threadCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return threadCursorPayload{}, fmt.Errorf("parse cursor: %w", err)
	}
	if payload.ID == "" {
		return threadCursorPayload{}, fmt.Errorf("invalid cursor")
	}
	return payload, nil
}

// EffectiveThreadLimit applies the same default/cap as ListThreads, so callers
// can decide whether a full page was returned (and thus a next cursor exists).
func EffectiveThreadLimit(limit int) int {
	if limit <= 0 {
		return defaultThreadLimit
	}
	if limit > maxThreadLimit {
		return maxThreadLimit
	}
	return limit
}
