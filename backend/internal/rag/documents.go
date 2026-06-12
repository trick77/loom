package rag

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const documentColumns = `id, user_id, project_id, thread_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at`

func (s *Store) CreateDocument(ctx context.Context, d Document) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (id, user_id, project_id, thread_id, artifact_id, volume_relpath, filename, mime, size_bytes, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.ProjectID, d.ThreadID, d.ArtifactID, d.VolumeRelpath, d.Filename, d.MIME, d.SizeBytes, d.Status,
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
	if err := row.Scan(&d.ID, &d.UserID, &d.ProjectID, &d.ThreadID, &d.ArtifactID, &d.VolumeRelpath, &d.Filename, &d.MIME, &d.SizeBytes, &d.Status, &d.Error, &createdAt, &embeddedAt); err != nil {
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

// HasIndexedChunks reports whether the user has any embedded document in the
// thread's knowledge scope: legacy user-global documents always, plus this
// project's documents (project thread) and this thread's private documents.
// Callers use it to skip embedding a query when there is nothing to retrieve.
func (s *Store) HasIndexedChunks(ctx context.Context, userID string, projectID, threadID *string) (bool, error) {
	// Mirror Retrieve's scope: global (project_id IS NULL AND thread_id IS NULL),
	// plus the project and/or the thread when present.
	query := `SELECT 1 FROM documents
		WHERE user_id = ? AND status = 'embedded'
		  AND ((project_id IS NULL AND thread_id IS NULL)`
	args := []any{userID}
	if projectID != nil {
		query += ` OR project_id = ?`
		args = append(args, *projectID)
	}
	if threadID != nil && *threadID != "" {
		query += ` OR thread_id = ?`
		args = append(args, *threadID)
	}
	query += `) LIMIT 1`
	var one int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check indexed chunks: %w", err)
	}
	return true, nil
}

// reconcileLegacyMarker records, in schema_migrations, that the one-time legacy
// scope reconciliation has run, so it never executes again.
const reconcileLegacyMarker = "reconcile_legacy_document_scopes_v1"

// ReconcileLegacyDocumentScopes fixes documents uploaded before thread-private
// scoping existed, which were stored user-global (project_id IS NULL, thread_id
// IS NULL) and therefore leaked into every project-less chat.
//
//   - Recoverable: a composer upload still linked to its originating artifact
//     (which records the source thread) is rebound to that thread — thread_id is
//     backfilled and the chunk embeddings are re-keyed to 'thread:<id>' so the
//     document is retrievable only in the chat it was uploaded in.
//   - Unrecoverable: a global document whose origin thread cannot be determined
//     (no linked artifact, or the artifact has no thread) cannot be migrated to a
//     specific chat, so it is deleted (chunks + embeddings) rather than left to
//     leak. When this ran there was no intentional user-global upload path, so
//     every such document was a stranded composer upload.
//
// It is a strict ONE-TIME data fix, gated on a schema_migrations marker: the
// blanket delete of globals must not fire again on later boots, so that a future
// deliberate global-upload feature (which would also be project/thread-less)
// cannot be silently wiped by it.
func (s *Store) ReconcileLegacyDocumentScopes(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Already reconciled on a prior boot? Then do nothing — never touch newer data.
	var done int
	switch err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = ?`, reconcileLegacyMarker).Scan(&done); err {
	case nil:
		return nil
	case sql.ErrNoRows:
		// not yet run — proceed
	default:
		return fmt.Errorf("check reconcile marker: %w", err)
	}

	// Recover: backfill thread_id from the originating artifact's provenance.
	if _, err := tx.ExecContext(ctx, `
		UPDATE documents
		SET thread_id = (
			SELECT a.thread_id FROM artifacts a
			WHERE a.user_id = documents.user_id AND a.id = documents.artifact_id
		)
		WHERE project_id IS NULL AND thread_id IS NULL AND artifact_id IS NOT NULL
		  AND EXISTS (
			SELECT 1 FROM artifacts a
			WHERE a.user_id = documents.user_id AND a.id = documents.artifact_id
			  AND a.thread_id IS NOT NULL AND a.thread_id != ''
		)`); err != nil {
		return fmt.Errorf("backfill thread scope: %w", err)
	}

	// Re-key the embeddings of just-recovered documents from the global ('') scope
	// to their thread scope. vec0 rejects metadata UPDATEs driven by a subquery,
	// so re-key one row at a time with a literal value.
	rekey, err := collectChunkScopes(ctx, tx,
		`SELECT c.id, d.thread_id FROM chunks c
		 JOIN documents d ON d.user_id = c.user_id AND d.id = c.document_id
		 WHERE d.project_id IS NULL AND d.thread_id IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("collect recovered chunks: %w", err)
	}
	for _, rk := range rekey {
		scope := threadScopePrefix + rk.threadID
		if _, err := tx.ExecContext(ctx,
			`UPDATE vec_chunks SET project_id = ? WHERE rowid = ? AND project_id != ?`,
			scope, rk.rowid, scope); err != nil {
			return fmt.Errorf("re-key chunk %d: %w", rk.rowid, err)
		}
	}

	// Delete unrecoverable globals: their embeddings first (a vtab is unreachable
	// by FK cascade), one row at a time, then the document rows (chunks cascade).
	strays, err := collectChunkScopes(ctx, tx,
		`SELECT c.id, '' FROM chunks c
		 JOIN documents d ON d.user_id = c.user_id AND d.id = c.document_id
		 WHERE d.project_id IS NULL AND d.thread_id IS NULL`)
	if err != nil {
		return fmt.Errorf("collect stranded chunks: %w", err)
	}
	for _, st := range strays {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vec_chunks WHERE rowid = ?`, st.rowid); err != nil {
			return fmt.Errorf("delete stranded embedding %d: %w", st.rowid, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM documents WHERE project_id IS NULL AND thread_id IS NULL`); err != nil {
		return fmt.Errorf("delete stranded documents: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`, reconcileLegacyMarker); err != nil {
		return fmt.Errorf("record reconcile marker: %w", err)
	}

	return tx.Commit()
}

type chunkScope struct {
	rowid    int64
	threadID string
}

// collectChunkScopes runs a query returning (chunk_rowid, thread_id) pairs and
// materialises them, so the caller can issue per-row vec_chunks writes afterwards
// (SQLite's single connection forbids interleaving a write with an open read).
func collectChunkScopes(ctx context.Context, tx *sql.Tx, query string) ([]chunkScope, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chunkScope
	for rows.Next() {
		var cs chunkScope
		if err := rows.Scan(&cs.rowid, &cs.threadID); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
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
