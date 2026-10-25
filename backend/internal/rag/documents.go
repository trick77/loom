package rag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const documentColumns = `id, user_id, project_id, thread_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at`

// CreateDocument inserts a new document record into the store.
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
	created, err := time.Parse(time.DateTime, createdAt)
	if err != nil {
		return Document{}, fmt.Errorf("parse document created_at %q: %w", createdAt, err)
	}
	d.CreatedAt = created
	if embeddedAt.Valid {
		t, err := time.Parse(time.DateTime, embeddedAt.String)
		if err != nil {
			return Document{}, fmt.Errorf("parse document embedded_at %q: %w", embeddedAt.String, err)
		}
		d.EmbeddedAt = &t
	}
	return d, nil
}

// GetDocument retrieves a document by ID for a given user, returning the document, whether it exists, and any error.
func (s *Store) GetDocument(ctx context.Context, userID, id string) (Document, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM documents WHERE user_id = ? AND id = ?`, userID, id)
	d, err := scanDocument(row)
	if errors.Is(err, sql.ErrNoRows) {
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
	defer func() { _ = rows.Close() }()
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

// CountThreadDocuments returns the number of thread-private documents for a given thread.
func (s *Store) CountThreadDocuments(ctx context.Context, userID, threadID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM documents WHERE user_id = ? AND project_id IS NULL AND thread_id = ?`,
		userID, threadID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count thread documents: %w", err)
	}
	return count, nil
}

// ArtifactIDsForThreadArtifactsInUse returns the ids of the thread's artifacts
// that still back a document row. Those are project- or user-global knowledge
// documents whose artifact only carries the thread id as provenance (see how
// Upload records it): their file and document row must survive the thread, so
// the delete path detaches them from it first. Letting the cascade reach such an
// artifact does not just orphan its document — it fires documents' ON DELETE SET
// NULL over the composite (user_id, artifact_id) key, nulling user_id too and
// aborting the whole thread delete on its NOT NULL constraint.
//
// Call after DeleteThreadScopeDocuments, so the thread's own private documents
// are already gone and cannot match here. Soft-deleted artifacts are deliberately
// not filtered out: their row and thread_id still exist, so the cascade would
// reach them and abort the delete just the same.
func (s *Store) ArtifactIDsForThreadArtifactsInUse(ctx context.Context, userID, threadID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT d.artifact_id
FROM documents d
JOIN artifacts a ON a.user_id = d.user_id AND a.id = d.artifact_id
WHERE d.user_id = ? AND a.thread_id = ?`, userID, threadID)
	if err != nil {
		return nil, fmt.Errorf("list in-use thread artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan in-use thread artifact: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateStatus updates a document's status and error message.
func (s *Store) UpdateStatus(ctx context.Context, userID, id, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = ? WHERE user_id = ? AND id = ?`,
		status, errMsg, userID, id)
	if err != nil {
		return fmt.Errorf("update document status: %w", err)
	}
	return nil
}

// SetDocumentFullText caches a document's extracted plain text on its row, so the
// inline/full-document paths can read it back without re-running extraction.
func (s *Store) SetDocumentFullText(ctx context.Context, userID, id, text string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE documents SET full_text = ? WHERE user_id = ? AND id = ?`,
		text, userID, id)
	if err != nil {
		return fmt.Errorf("set document full text: %w", err)
	}
	return nil
}

// DocumentIDForArtifact returns the id of the user's document backed by the
// given artifact, if any.
func (s *Store) DocumentIDForArtifact(ctx context.Context, userID, artifactID string) (string, bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM documents WHERE user_id = ? AND artifact_id = ? LIMIT 1`,
		userID, artifactID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("document for artifact: %w", err)
	}
	return id, true, nil
}

// GetDocumentFullText returns the cached extracted text for a document, or "" when
// the document does not exist or was indexed before text caching (callers then
// fall back to live extraction). full_text is intentionally excluded from
// documentColumns so List/Get stay cheap; this is a targeted single-row read.
func (s *Store) GetDocumentFullText(ctx context.Context, userID, id string) (string, error) {
	var text string
	err := s.db.QueryRowContext(ctx,
		`SELECT full_text FROM documents WHERE user_id = ? AND id = ?`,
		userID, id).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get document full text: %w", err)
	}
	return text, nil
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
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check indexed chunks: %w", err)
	}
	return true, nil
}

// maxDocumentsInScope bounds DocumentsInScope. The thread's own documents sort
// first, so the cap only ever trims old project and user-global ones.
const maxDocumentsInScope = 1000

// DocumentsInScope returns up to limit (at most maxDocumentsInScope) documents
// a thread can use: its own, its project's and the user-global ones (the scope
// IndexedDocsInScope uses for knowledge), whatever their indexing status. The
// thread's come first, then the project's, then the global ones, each newest
// first (id breaks ties: created_at has one-second resolution).
func (s *Store) DocumentsInScope(ctx context.Context, userID string, projectID, threadID *string, limit int) ([]Document, error) {
	if limit <= 0 || limit > maxDocumentsInScope {
		limit = maxDocumentsInScope
	}
	query := `SELECT ` + documentColumns + ` FROM documents
		WHERE user_id = ? AND ((project_id IS NULL AND thread_id IS NULL)`
	args := []any{userID}
	if projectID != nil {
		query += ` OR project_id = ?`
		args = append(args, *projectID)
	}
	if threadID != nil && *threadID != "" {
		query += ` OR thread_id = ?`
		args = append(args, *threadID)
	}
	thread := ""
	if threadID != nil {
		thread = *threadID
	}
	query += `) ORDER BY CASE WHEN thread_id = ? THEN 0 WHEN project_id IS NOT NULL THEN 1 ELSE 2 END, created_at DESC, id LIMIT ?`
	args = append(args, thread, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list documents in scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
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

// IndexedDocsInScope returns every embedded document in the thread's knowledge
// scope (mirroring Retrieve/HasIndexedChunks: global, plus the project and/or the
// thread when present), each with the sum of its chunks' token counts. Ordered
// smallest-first so a budgeted caller can fit as many whole documents as possible.
func (s *Store) IndexedDocsInScope(ctx context.Context, userID string, projectID, threadID *string) ([]IndexedDoc, error) {
	query := `SELECT d.id, d.filename, COALESCE(SUM(c.token_count), 0) AS tokens
		FROM documents d
		LEFT JOIN chunks c ON c.document_id = d.id AND c.user_id = d.user_id
		WHERE d.user_id = ? AND d.status = 'embedded'
		  AND ((d.project_id IS NULL AND d.thread_id IS NULL)`
	args := []any{userID}
	if projectID != nil {
		query += ` OR d.project_id = ?`
		args = append(args, *projectID)
	}
	if threadID != nil && *threadID != "" {
		query += ` OR d.thread_id = ?`
		args = append(args, *threadID)
	}
	query += `) GROUP BY d.id, d.filename ORDER BY tokens ASC, d.created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list indexed docs in scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var docs []IndexedDoc
	for rows.Next() {
		var d IndexedDoc
		if err := rows.Scan(&d.ID, &d.Filename, &d.TokenCount); err != nil {
			return nil, fmt.Errorf("scan indexed doc: %w", err)
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
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
