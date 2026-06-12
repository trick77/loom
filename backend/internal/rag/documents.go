package rag

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const documentColumns = `id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at`

func (s *Store) CreateDocument(ctx context.Context, d Document) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.ProjectID, d.ArtifactID, d.VolumeRelpath, d.Filename, d.MIME, d.SizeBytes, d.Status,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func scanDocument(row interface{ Scan(...any) error }) (Document, error) {
	var d Document
	var createdAt string
	var embeddedAt sql.NullString
	if err := row.Scan(&d.ID, &d.UserID, &d.ProjectID, &d.ArtifactID, &d.VolumeRelpath, &d.Filename, &d.MIME, &d.SizeBytes, &d.Status, &d.Error, &createdAt, &embeddedAt); err != nil {
		return Document{}, err
	}
	d.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	if embeddedAt.Valid {
		if t, err := time.Parse(time.DateTime, embeddedAt.String); err == nil {
			d.EmbeddedAt = &t
		}
	}
	return d, nil
}

func (s *Store) GetDocument(ctx context.Context, userID, id string) (Document, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM documents WHERE user_id = ? AND id = ?`, userID, id)
	d, err := scanDocument(row)
	if err == sql.ErrNoRows {
		return Document{}, false, nil
	}
	if err != nil {
		return Document{}, false, fmt.Errorf("get document: %w", err)
	}
	return d, true, nil
}

// ListDocuments returns a user's documents, newest first. If projectID is
// non-nil, only documents in that project are returned.
func (s *Store) ListDocuments(ctx context.Context, userID string, projectID *string) ([]Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE user_id = ?`
	args := []any{userID}
	if projectID != nil {
		query += ` AND project_id = ?`
		args = append(args, *projectID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()
	var docs []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func (s *Store) UpdateStatus(ctx context.Context, userID, id, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = ? WHERE user_id = ? AND id = ?`,
		status, errMsg, userID, id)
	if err != nil {
		return fmt.Errorf("update document status: %w", err)
	}
	return nil
}

// ResetStuckIngestions marks documents left mid-ingestion (extracting/embedding)
// by a crash or restart as errored, so they aren't stranded in a transient
// state. Called once at boot.
func (s *Store) ResetStuckIngestions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = 'ingestion interrupted by restart'
		 WHERE status IN (?, ?)`,
		StatusError, StatusExtracting, StatusEmbedding)
	if err != nil {
		return fmt.Errorf("reset stuck ingestions: %w", err)
	}
	return nil
}
