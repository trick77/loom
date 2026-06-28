package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// CreateShare inserts a new public share row for a thread. The caller supplies the
// opaque ShareID (generated up front so the snapshot can bake absolute, share-
// scoped artifact URLs) and must ensure no share already exists for the thread —
// the thread_id UNIQUE constraint enforces one-share-per-thread regardless. A
// share_id collision is ~2^-128 and surfaces as a UNIQUE error for the caller.
func (s *Store) CreateShare(ctx context.Context, userID string, in CreateShareInput) (Share, error) {
	artifactIDs, err := marshalArtifactIDs(in.ArtifactIDs)
	if err != nil {
		return Share{}, err
	}
	snapshot := string(in.Snapshot)
	if strings.TrimSpace(snapshot) == "" {
		snapshot = "{}"
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO shared_threads (id, share_id, thread_id, user_id, title, snapshot, artifact_ids)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		newID(), in.ShareID, in.ThreadID, userID, in.Title, snapshot, artifactIDs,
	)
	if err != nil {
		return Share{}, fmt.Errorf("insert share: %w", err)
	}
	share, ok, err := s.GetShareByThreadID(ctx, userID, in.ThreadID)
	if err != nil {
		return Share{}, err
	}
	if !ok {
		return Share{}, fmt.Errorf("inserted share not found")
	}
	return share, nil
}

// GetShareByThreadID returns the owner's share row for a thread.
func (s *Store) GetShareByThreadID(ctx context.Context, userID, threadID string) (Share, bool, error) {
	return s.getShare(ctx, `WHERE user_id = ? AND thread_id = ?`, userID, threadID)
}

// GetShareByShareID looks a share up by its public token. It is intentionally NOT
// user-scoped — this is the public read boundary. Callers MUST check Share.Shared
// before exposing anything; a disabled share must behave like a missing one.
func (s *Store) GetShareByShareID(ctx context.Context, shareID string) (Share, bool, error) {
	return s.getShare(ctx, `WHERE share_id = ?`, shareID)
}

func (s *Store) getShare(ctx context.Context, where string, args ...any) (Share, bool, error) {
	query := `
SELECT id, share_id, thread_id, user_id, shared, title, snapshot, artifact_ids, snapshot_at, created_at, updated_at
FROM shared_threads
` + where
	share, err := scanShare(s.db.QueryRowContext(ctx, query, args...))
	if err == nil {
		return share, true, nil
	}
	if err == sql.ErrNoRows {
		return Share{}, false, nil
	}
	return Share{}, false, fmt.Errorf("get share: %w", err)
}

// UpdateShareSnapshot re-freezes the share with a fresh snapshot (same share_id)
// and re-enables it. Used by "Update share".
func (s *Store) UpdateShareSnapshot(ctx context.Context, userID, threadID string, in UpdateShareInput) (Share, bool, error) {
	artifactIDs, err := marshalArtifactIDs(in.ArtifactIDs)
	if err != nil {
		return Share{}, false, err
	}
	snapshot := string(in.Snapshot)
	if strings.TrimSpace(snapshot) == "" {
		snapshot = "{}"
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE shared_threads
SET title = ?, snapshot = ?, artifact_ids = ?, shared = 1, snapshot_at = datetime('now'), updated_at = datetime('now')
WHERE user_id = ? AND thread_id = ?`,
		in.Title, snapshot, artifactIDs, userID, threadID,
	)
	if err != nil {
		return Share{}, false, fmt.Errorf("update share: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Share{}, false, fmt.Errorf("update share rows: %w", err)
	}
	if affected == 0 {
		return Share{}, false, nil
	}
	return s.GetShareByThreadID(ctx, userID, threadID)
}

// SetShareEnabled toggles the public link on/off without dropping the snapshot.
func (s *Store) SetShareEnabled(ctx context.Context, userID, threadID string, enabled bool) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE shared_threads
SET shared = ?, updated_at = datetime('now')
WHERE user_id = ? AND thread_id = ?`,
		boolToInt(enabled), userID, threadID,
	)
	if err != nil {
		return false, fmt.Errorf("set share enabled: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set share enabled rows: %w", err)
	}
	return affected > 0, nil
}

// ListSharesForUser returns the user's shares, newest first, for the settings
// dashboard. Snapshots are omitted from the scan-heavy listing path is not needed
// here — the full row is small enough and the dashboard renders metadata only.
func (s *Store) ListSharesForUser(ctx context.Context, userID string) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, share_id, thread_id, user_id, shared, title, snapshot, artifact_ids, snapshot_at, created_at, updated_at
FROM shared_threads
WHERE user_id = ?
ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	defer rows.Close()
	var shares []Share
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, fmt.Errorf("scan share: %w", err)
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shares: %w", err)
	}
	return shares, nil
}

func scanShare(row rowScanner) (Share, error) {
	var share Share
	var shared int64
	var snapshot, artifactIDs string
	var snapshotAt, createdAt, updatedAt string
	if err := row.Scan(
		&share.ID,
		&share.ShareID,
		&share.ThreadID,
		&share.UserID,
		&shared,
		&share.Title,
		&snapshot,
		&artifactIDs,
		&snapshotAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Share{}, err
	}
	share.Shared = shared != 0
	share.Snapshot = json.RawMessage(snapshot)
	ids, err := unmarshalArtifactIDs(artifactIDs)
	if err != nil {
		return Share{}, fmt.Errorf("parse artifact_ids: %w", err)
	}
	share.ArtifactIDs = ids
	share.SnapshotAt, err = parseSQLiteTime(snapshotAt)
	if err != nil {
		return Share{}, fmt.Errorf("parse snapshot_at: %w", err)
	}
	share.CreatedAt, err = parseSQLiteTime(createdAt)
	if err != nil {
		return Share{}, fmt.Errorf("parse created_at: %w", err)
	}
	share.UpdatedAt, err = parseSQLiteTime(updatedAt)
	if err != nil {
		return Share{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return share, nil
}

func marshalArtifactIDs(ids []string) (string, error) {
	if len(ids) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("marshal artifact_ids: %w", err)
	}
	return string(encoded), nil
}

func unmarshalArtifactIDs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ContainsArtifactID reports whether id is in the share's generated-artifact
// allowlist — the authorization check for public artifact serving.
func (sh Share) ContainsArtifactID(id string) bool {
	for _, candidate := range sh.ArtifactIDs {
		if candidate == id {
			return true
		}
	}
	return false
}
