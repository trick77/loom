package rag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// vecWidthPattern reads the embedding width out of vec_chunks' DDL.
var vecWidthPattern = regexp.MustCompile(`embedding\s+float\[(\d+)\]`)

// VectorWidth is the width vec_chunks was created with. It must equal the
// embedding model's width; RebuildVectorTable changes it when the model does.
func (s *Store) VectorWidth(ctx context.Context) (int, error) {
	var ddl string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name = 'vec_chunks'`).Scan(&ddl); err != nil {
		return 0, fmt.Errorf("read vec_chunks schema: %w", err)
	}
	m := vecWidthPattern.FindStringSubmatch(ddl)
	if m == nil {
		return 0, fmt.Errorf("vec_chunks schema has no embedding width: %q", ddl)
	}
	return strconv.Atoi(m[1])
}

// RebuildVectorTable recreates vec_chunks at width, dropping every vector and
// keeping the chunks: their text is what re-embedding reads (see
// ChunksMissingVectors). Retrieval finds nothing for a chunk until its vector
// is back.
func (s *Store) RebuildVectorTable(ctx context.Context, width int) error {
	if width <= 0 {
		return fmt.Errorf("vector width %d is not positive", width)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DROP TABLE vec_chunks`); err != nil {
		return fmt.Errorf("drop vec_chunks: %w", err)
	}
	// Another model may take what the last one refused.
	if _, err := tx.ExecContext(ctx, `DELETE FROM vector_refused`); err != nil {
		return fmt.Errorf("clear refused chunks: %w", err)
	}
	// Same columns as the migration that created it; only the width moves.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`CREATE VIRTUAL TABLE vec_chunks USING vec0(
    embedding  float[%d],
    user_id    TEXT partition key,
    project_id TEXT
)`, width)); err != nil {
		return fmt.Errorf("create vec_chunks: %w", err)
	}
	return tx.Commit()
}

// VectorModel is the embedding model recorded as having written vec_chunks;
// ok is false when none is recorded yet.
func (s *Store) VectorModel(ctx context.Context) (model string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT model FROM vector_model WHERE id = 1`).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read vector model: %w", err)
	}
	return model, true, nil
}

// SetVectorModel records the embedding model that writes vec_chunks.
func (s *Store) SetVectorModel(ctx context.Context, model string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO vector_model (id, model) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET model = excluded.model`,
		model); err != nil {
		return fmt.Errorf("record vector model: %w", err)
	}
	return nil
}

// HasVectors reports whether vec_chunks holds any vector.
func (s *Store) HasVectors(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM vec_chunks LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check vectors: %w", err)
	}
	return true, nil
}

// MarkRefused records chunks the embedding model refused outright, so later
// re-embedding runs skip them.
func (s *Store) MarkRefused(ctx context.Context, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range chunkIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO vector_refused (chunk_id) VALUES (?)`, id); err != nil {
			return fmt.Errorf("mark refused chunk: %w", err)
		}
	}
	return tx.Commit()
}

// MissingVector is a stored chunk without an embedding.
type MissingVector struct {
	ChunkID int64
	UserID  string
	Text    string
	scope   string
}

// ChunksMissingVectors lists up to limit chunks after afterID that have no row
// in vec_chunks and were not refused by the model (see MarkRefused), in id
// order: pass the last id of one page to get the next.
func (s *Store) ChunksMissingVectors(ctx context.Context, afterID int64, limit int) ([]MissingVector, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.user_id, c.text, d.project_id, d.thread_id
FROM chunks c
JOIN documents d ON d.user_id = c.user_id AND d.id = c.document_id
WHERE c.id > ? AND NOT EXISTS (SELECT 1 FROM vec_chunks v WHERE v.rowid = c.id)
  AND NOT EXISTS (SELECT 1 FROM vector_refused r WHERE r.chunk_id = c.id)
ORDER BY c.id
LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chunks missing vectors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MissingVector
	for rows.Next() {
		var m MissingVector
		var projectID, threadID *string
		if err := rows.Scan(&m.ChunkID, &m.UserID, &m.Text, &projectID, &threadID); err != nil {
			return nil, err
		}
		m.scope = scopeKey(projectID, threadID)
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertVectors writes one vector per chunk, aligned to chunks, keyed and
// scoped exactly as ReplaceChunks writes them. The chunks were listed before
// their embedding call; a chunk deleted or re-indexed meanwhile (its id may
// even have been reused by a new chunk that already has a vector) is skipped,
// not written and not an error.
func (s *Store) InsertVectors(ctx context.Context, chunks []MissingVector, vectors [][]float32) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("chunk/vector count mismatch: %d vs %d", len(chunks), len(vectors))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, c := range chunks {
		var still int
		if err := tx.QueryRowContext(ctx, `
SELECT count(*) FROM chunks c
WHERE c.id = ? AND c.user_id = ? AND c.text = ?
  AND NOT EXISTS (SELECT 1 FROM vec_chunks v WHERE v.rowid = c.id)`,
			c.ChunkID, c.UserID, c.Text).Scan(&still); err != nil {
			return fmt.Errorf("check chunk: %w", err)
		}
		if still == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (?, ?, ?, ?)`,
			c.ChunkID, vecLiteral(vectors[i]), c.UserID, c.scope); err != nil {
			return fmt.Errorf("insert vector: %w", err)
		}
	}
	return tx.Commit()
}
