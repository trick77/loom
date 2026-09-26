package rag

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// VecCompaction is what one CompactVectors call found and did. Chunks are
// vec0's storage chunks (vec_chunks_chunks rows), not loom's chunks.
type VecCompaction struct {
	Compacted    bool
	Rows         int64
	ChunksBefore int64
	ChunksAfter  int64
}

// CompactVectors rebuilds vec_chunks when deleted vectors have left it bloated.
//
// vec0 never gives space back. A delete clears a validity bit, the storage
// chunk stays, and an insert only appends to its partition's newest chunk, so
// every re-index, unindex and document, thread or project delete leaves chunks
// nothing reclaims. A KNN query reads every chunk of the user's partition
// whole, live slot or not: rongo held 46k vectors in 380 chunks (46 needed)
// and spent minutes per search.
//
// Bloated means more than twice the chunks the live rows need, per partition.
// The rebuild keeps every rowid, vector, user_id and project_id, so nothing is
// re-embedded and retrieval is unaffected. One transaction: a failure leaves
// the table as it was.
//
// Boot only. The transaction holds the write lock for the whole copy — 9.6 s
// for 14k vectors on a laptop in rongo — past the busy timeout, so run beside
// live requests it fails their writes. Deletes warn instead (WarnVectorBloat).
func (s *Store) CompactVectors(ctx context.Context) (VecCompaction, error) {
	return s.compactVectors(ctx, nil)
}

// compactVectors is CompactVectors; before, when set, is called once the
// table is known to be bloated and before the rebuild starts.
func (s *Store) compactVectors(ctx context.Context, before func(VecCompaction, int64)) (VecCompaction, error) {
	c, needed, err := s.measureVectors(ctx)
	if err != nil || c.ChunksBefore <= 2*needed {
		c.ChunksAfter = c.ChunksBefore
		return c, err
	}
	if before != nil {
		before(c, needed)
	}

	var ddl string
	if err := s.db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'vec_chunks'`).Scan(&ddl); err != nil {
		return c, fmt.Errorf("read vec_chunks schema: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback() }()
	// Opens with a write to main: a WAL transaction that reads first holds a
	// snapshot, and its first write after another connection committed fails
	// at once as "database is locked". The copy table is in main for that
	// reason, not in temp.
	for _, q := range []string{
		`CREATE TABLE vec_compact_keep (id INTEGER PRIMARY KEY, embedding BLOB NOT NULL, user_id TEXT NOT NULL, project_id TEXT)`,
		`INSERT INTO vec_compact_keep (id, embedding, user_id, project_id) SELECT rowid, embedding, user_id, project_id FROM vec_chunks`,
		`DROP TABLE vec_chunks`,
		ddl,
		`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id)
		 SELECT id, embedding, user_id, project_id FROM vec_compact_keep ORDER BY user_id, id`,
		`DROP TABLE vec_compact_keep`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return c, fmt.Errorf("compact vec_chunks: %w", err)
		}
	}
	var rows int64
	if err := tx.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM vec_chunks_rowids), (SELECT COUNT(*) FROM vec_chunks_chunks)`).
		Scan(&rows, &c.ChunksAfter); err != nil {
		return c, fmt.Errorf("count compacted vec_chunks: %w", err)
	}
	if rows != c.Rows {
		return c, fmt.Errorf("compact vec_chunks: %d rows after, %d before", rows, c.Rows)
	}
	if err := tx.Commit(); err != nil {
		return c, err
	}
	c.Compacted = true
	return c, nil
}

// measureVectors counts vec_chunks' live rows and storage chunks, and the
// chunks those rows need: per partition, since a partition never shares a
// chunk. An empty table needs none and has none.
func (s *Store) measureVectors(ctx context.Context) (c VecCompaction, needed int64, err error) {
	var size sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM vec_chunks_rowids),
		       (SELECT COUNT(*) FROM vec_chunks_chunks),
		       (SELECT length(validity) * 8 FROM vec_chunks_chunks LIMIT 1)`).
		Scan(&c.Rows, &c.ChunksBefore, &size); err != nil {
		return c, 0, fmt.Errorf("measure vec_chunks: %w", err)
	}
	if !size.Valid || size.Int64 <= 0 {
		return c, 0, nil
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM((n + ? - 1) / ?), 0) FROM (
			SELECT COUNT(*) AS n FROM vec_chunks_rowids r
			JOIN vec_chunks_chunks k ON k.chunk_id = r.chunk_id
			GROUP BY k.partition00)`, size.Int64, size.Int64).Scan(&needed); err != nil {
		return c, 0, fmt.Errorf("measure vec_chunks partitions: %w", err)
	}
	return c, needed, nil
}

// WarnVectorBloat is what a delete does instead of compacting: it says the
// table is bloated, and that the next boot compacts it. Quiet when healthy.
// args name the occasion (the document, the thread, the project).
func (s *Store) WarnVectorBloat(ctx context.Context, args ...any) {
	c, needed, err := s.measureVectors(ctx)
	if err != nil {
		slog.WarnContext(ctx, "measuring the vector index failed", append(args, "err", err)...)
		return
	}
	if c.ChunksBefore > 2*needed {
		slog.WarnContext(ctx, "vector index bloated, compacted at next boot", append(args, "rows", c.Rows,
			"chunks", c.ChunksBefore, "chunks_needed", needed)...)
	}
}

// CompactVectorsAtBoot compacts when bloated and vacuums after a compaction,
// logging what it did; a healthy table states its shape, since rows against
// storage chunks is the one number that shows vec0 bloat before searches slow
// down. A failure is a warning: the index is complete, bloat only costs time.
//
// The rebuild and the vacuum each announce themselves first: a boot sitting
// silent for minutes reads as a hang.
func (s *Store) CompactVectorsAtBoot(ctx context.Context) {
	start := time.Now()
	c, err := s.compactVectors(ctx, func(c VecCompaction, needed int64) {
		start = time.Now()
		slog.InfoContext(ctx, "vector index bloated, compacting before listening; this can take minutes",
			"rows", c.Rows, "chunks", c.ChunksBefore, "chunks_needed", needed)
	})
	switch {
	case err != nil:
		slog.WarnContext(ctx, "compacting the vector index failed", "err", err)
	case c.Compacted:
		slog.InfoContext(ctx, "vector index compacted", "rows", c.Rows,
			"chunks_before", c.ChunksBefore, "chunks_after", c.ChunksAfter, "took", took(start))
		s.vacuum(ctx)
	default:
		slog.InfoContext(ctx, "vector index", "rows", c.Rows, "chunks", c.ChunksBefore)
	}
}

// vacuum rewrites the database file so the pages a compaction freed go back
// to the disk, and logs the size either side. It blocks writers while it
// runs, so it belongs at boot, before anything else opens a transaction.
func (s *Store) vacuum(ctx context.Context) {
	start := time.Now()
	before, err := s.dbBytes(ctx)
	if err == nil {
		slog.InfoContext(ctx, "vacuuming the database; this can take minutes", "bytes_before", before)
		start = time.Now()
		_, err = s.db.ExecContext(ctx, `VACUUM`)
	}
	if err == nil {
		// The rewrite lands in the WAL; the checkpoint is what shrinks the file.
		_, err = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	var after int64
	if err == nil {
		after, err = s.dbBytes(ctx)
	}
	if err != nil {
		slog.WarnContext(ctx, "vacuuming the database failed", "err", err)
		return
	}
	slog.InfoContext(ctx, "database vacuumed", "bytes_before", before, "bytes_after", after, "took", took(start))
}

func (s *Store) dbBytes(ctx context.Context) (int64, error) {
	var pages, size int64
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, fmt.Errorf("read page_count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size); err != nil {
		return 0, fmt.Errorf("read page_size: %w", err)
	}
	return pages * size, nil
}

// took is a duration for a log line. As .String(): a JSON handler renders a
// time.Duration as integer nanoseconds.
func took(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}
