package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/trick77/spark/internal/chat"
)

type Store struct {
	db *sql.DB
}

type CreateInput struct {
	UserID          string
	ThreadID        string
	ProjectID       *string
	MessageID       *string
	DisplayFilename string
	VolumeRelPath   string
	MIMEType        string
	SizeBytes       int64
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in CreateInput) (Artifact, error) {
	id := chat.NewIDForInternalUse()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, project_id, message_id, display_filename, volume_relpath, mime_type, size_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.UserID, in.ThreadID, in.ProjectID, in.MessageID, in.DisplayFilename, in.VolumeRelPath, in.MIMEType, in.SizeBytes,
	)
	if err != nil {
		return Artifact{}, fmt.Errorf("insert artifact: %w", err)
	}
	artifact, ok, err := s.Get(ctx, in.UserID, id)
	if err != nil {
		return Artifact{}, err
	}
	if !ok {
		return Artifact{}, fmt.Errorf("inserted artifact not found")
	}
	return artifact, nil
}

func (s *Store) Get(ctx context.Context, userID, artifactID string) (Artifact, bool, error) {
	var out Artifact
	var createdAt string
	var projectID, messageID sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, user_id, thread_id, project_id, message_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts
WHERE user_id = ? AND id = ?`, userID, artifactID).Scan(
		&out.ID,
		&out.UserID,
		&out.ThreadID,
		&projectID,
		&messageID,
		&out.DisplayFilename,
		&out.VolumeRelPath,
		&out.MIMEType,
		&out.SizeBytes,
		&out.Source,
		&createdAt,
	)
	if err == sql.ErrNoRows {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, fmt.Errorf("get artifact: %w", err)
	}
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	if messageID.Valid {
		out.MessageID = &messageID.String
	}
	parsed, err := parseSQLiteTime(createdAt)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	out.DownloadURL = "/api/artifacts/" + out.ID + "/download"
	return out, true, nil
}

func parseSQLiteTime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}
