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

	var projectID *string
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM documents WHERE user_id = ? AND id = ?`, userID, documentID).Scan(&projectID); err != nil {
		return fmt.Errorf("load document scope: %w", err)
	}
	scope := scopeValue(projectID)

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
