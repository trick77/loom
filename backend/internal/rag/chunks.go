package rag

import (
	"context"
	"database/sql"
	"fmt"
)

// deleteChunksTx removes a document's chunks and their vec rows within tx. The
// vec rows must be deleted explicitly (CASCADE/triggers cannot reach a vtab).
func deleteChunksTx(ctx context.Context, tx *sql.Tx, userID, documentID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM chunks WHERE user_id = ? AND document_id = ?`, userID, documentID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vec_chunks WHERE rowid = ?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE user_id = ? AND document_id = ?`, userID, documentID); err != nil {
		return err
	}
	return nil
}

// ReplaceChunks atomically replaces a document's chunks and embeddings, then
// marks the document embedded. len(chunks) must equal len(embeddings).
func (s *Store) ReplaceChunks(ctx context.Context, userID, documentID string, chunks []TextChunk, embeddings [][]float32) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("chunk/embedding count mismatch: %d vs %d", len(chunks), len(embeddings))
	}

	var projectID, threadID *string
	if err := s.db.QueryRowContext(ctx, `SELECT project_id, thread_id FROM documents WHERE user_id = ? AND id = ?`, userID, documentID).Scan(&projectID, &threadID); err != nil {
		return fmt.Errorf("load document scope: %w", err)
	}
	scope := scopeKey(projectID, threadID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteChunksTx(ctx, tx, userID, documentID); err != nil {
		return fmt.Errorf("clear existing chunks: %w", err)
	}

	for i, c := range chunks {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chunks (document_id, user_id, project_id, ordinal, text, token_count)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			documentID, userID, projectID, c.Ordinal, c.Text, c.TokenCount)
		if err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("chunk rowid: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (?, ?, ?, ?)`,
			rowid, vecLiteral(embeddings[i]), userID, scope); err != nil {
			return fmt.Errorf("insert embedding: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', embedded_at = datetime('now') WHERE user_id = ? AND id = ?`,
		StatusEmbedded, userID, documentID); err != nil {
		return fmt.Errorf("mark embedded: %w", err)
	}
	return tx.Commit()
}

// ClearChunks removes a document's chunks and embeddings but keeps the document
// row, so it can be re-indexed later (used by "unindex").
func (s *Store) ClearChunks(ctx context.Context, userID, documentID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteChunksTx(ctx, tx, userID, documentID); err != nil {
		return fmt.Errorf("clear chunks: %w", err)
	}
	return tx.Commit()
}

// collectChunkRowids materialises chunk rowids from a query so the caller can
// issue per-row vec_chunks deletes afterwards (SQLite forbids interleaving a
// write with an open read on its single connection).
func collectChunkRowids(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DeleteThreadScopeDocuments removes every thread-private document, its chunks,
// and their embeddings for a deleted chat, in one transaction. Thread-private
// documents are exactly those with no project and this thread_id (see how
// Upload assigns scope); project documents are never matched here.
//
// The embeddings live in a vec0 vtab unreachable by FK cascade, so their rowids
// are collected while the chunk rows still exist, deleted one at a time, then
// the chunk and document rows are removed.
func (s *Store) DeleteThreadScopeDocuments(ctx context.Context, userID, threadID string) error {
	const scope = `d.user_id = ? AND d.project_id IS NULL AND d.thread_id = ?`
	return s.deleteScopeDocuments(ctx, scope, userID, threadID)
}

// DeleteProjectScopeDocuments removes every document, chunk, and embedding for a
// deleted project, in one transaction. It MUST run before the project row is
// deleted: the projects FK cascade would otherwise drop the chunk rows and lose
// the rowids needed to clear the vec0 embeddings. Once this has run the later
// project DELETE cascade is a no-op for these documents.
//
// It catches two kinds of document: project-scoped ones (project_id = ?) and
// thread-private ones (project_id NULL, thread_id set) whose thread now lives in
// the project. The latter arise when a project-less thread with an attachment is
// moved into a project — the document keeps its thread scope, so it is invisible
// to the project_id filter and, with no FK on thread_id, also escapes cascade.
// Relies on the project's threads still existing, i.e. on running before
// chat.DeleteProject.
func (s *Store) DeleteProjectScopeDocuments(ctx context.Context, userID, projectID string) error {
	const scope = `d.user_id = ? AND (
		d.project_id = ?
		OR (d.project_id IS NULL AND d.thread_id IN (
			SELECT t.id FROM threads t WHERE t.user_id = ? AND t.project_id = ?)))`
	return s.deleteScopeDocuments(ctx, scope, userID, projectID, userID, projectID)
}

// deleteScopeDocuments deletes the embeddings, chunks, and documents matching a
// documents-table predicate (referenced as alias d) in one transaction, in the
// vtab-safe order: collect rowids, delete vec_chunks, delete chunks, delete
// documents. args bind the predicate's placeholders.
func (s *Store) deleteScopeDocuments(ctx context.Context, scope string, args ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rowids, err := collectChunkRowids(ctx,
		tx,
		`SELECT c.id FROM chunks c
		 JOIN documents d ON d.user_id = c.user_id AND d.id = c.document_id
		 WHERE `+scope,
		args...)
	if err != nil {
		return fmt.Errorf("collect scope chunks: %w", err)
	}
	for _, id := range rowids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vec_chunks WHERE rowid = ?`, id); err != nil {
			return fmt.Errorf("delete scope embedding %d: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM chunks WHERE id IN (
			SELECT c.id FROM chunks c
			JOIN documents d ON d.user_id = c.user_id AND d.id = c.document_id
			WHERE `+scope+`)`,
		args...); err != nil {
		return fmt.Errorf("delete scope chunks: %w", err)
	}
	// Reuse the same predicate against the documents table; alias d via a self
	// scope so the shared predicate string applies unchanged.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM documents WHERE id IN (SELECT d.id FROM documents d WHERE `+scope+`)`,
		args...); err != nil {
		return fmt.Errorf("delete scope documents: %w", err)
	}
	return tx.Commit()
}

// DeleteDocument removes a document, its chunks, and their embeddings.
func (s *Store) DeleteDocument(ctx context.Context, userID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteChunksTx(ctx, tx, userID, id); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE user_id = ? AND id = ?`, userID, id); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return tx.Commit()
}
