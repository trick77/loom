package rag

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"sync"
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
// Boot only. The transaction holds the write lock for the whole copy — 8m33s
// for rongo's 46k vectors — past the busy timeout, so run beside live
// requests it fails their writes. Deletes warn instead (WarnVectorBloat).
func (s *Store) CompactVectors(ctx context.Context) (VecCompaction, error) {
	return s.compactVectors(ctx, nil)
}

// compactBatch is how many vectors one copy statement moves. Progress is
// counted in these, so a rewrite of minutes reports rows as it goes. A var so
// a test can walk many batches over a small table.
var compactBatch = 2000

// compactHeartbeat is how often a running compaction or vacuum says it is
// still running. A var so a test need not wait for it.
var compactHeartbeat = 15 * time.Second

// compactProgress is what a running compaction has got to, read by the
// heartbeat. A nil one records nothing.
type compactProgress struct {
	ctx   context.Context
	args  []any
	mu    sync.Mutex
	step  string
	done  int64
	total int64
}

func (p *compactProgress) begin(step string, total int64, attrs ...any) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.step, p.done, p.total = step, 0, total
	p.mu.Unlock()
	slog.InfoContext(p.ctx, step, append(slices.Clone(p.args), attrs...)...)
}

func (p *compactProgress) add(n int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.done += n
	p.mu.Unlock()
}

func (p *compactProgress) state() (step string, done, total int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.step, p.done, p.total
}

// heartbeat logs msg every compactHeartbeat until the returned stop is
// called, with whatever attrs returns at that moment and the time elapsed.
func heartbeat(ctx context.Context, msg string, attrs func() []any) (stop func()) {
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(compactHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				slog.InfoContext(ctx, msg, append(attrs(), "elapsed", took(start))...)
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

func (s *Store) compactVectors(ctx context.Context, p *compactProgress) (VecCompaction, error) {
	c, needed, err := s.measureVectors(ctx)
	if err != nil || c.ChunksBefore <= 2*needed {
		c.ChunksAfter = c.ChunksBefore
		return c, err
	}

	var ddl string
	if err := s.db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'vec_chunks'`).Scan(&ddl); err != nil {
		return c, fmt.Errorf("read vec_chunks schema: %w", err)
	}
	p.begin("compacting the vector index", c.Rows,
		"rows", c.Rows, "chunks", c.ChunksBefore, "chunks_needed", needed)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback() }()
	exec := func(q string, args ...any) (int64, error) {
		res, err := tx.ExecContext(ctx, q, args...)
		if err != nil {
			return 0, fmt.Errorf("compact vec_chunks: %w", err)
		}
		return res.RowsAffected()
	}
	// Opens with a write to main: a WAL transaction that reads first holds a
	// snapshot, and its first write after another connection committed fails
	// at once as "database is locked". The copy table is in main for that
	// reason, not in temp.
	if _, err := exec(`CREATE TABLE vec_compact_keep (id INTEGER PRIMARY KEY, embedding BLOB NOT NULL, user_id TEXT NOT NULL, project_id TEXT)`); err != nil {
		return c, err
	}
	// Both copies go in rowid batches. The partition key and the metadata
	// travel with the vector.
	p.begin("copying vectors out", c.Rows)
	if err := copyBatches(ctx, tx, p, exec, copyOutSQL, `SELECT MAX(id) FROM vec_compact_keep`); err != nil {
		return c, err
	}
	if _, err := exec(`DROP TABLE vec_chunks`); err != nil {
		return c, err
	}
	if _, err := exec(ddl); err != nil {
		return c, err
	}
	p.begin("writing vectors back", c.Rows)
	if err := copyBatches(ctx, tx, p, exec, `INSERT INTO vec_chunks (rowid, embedding, user_id, project_id)
		SELECT id, embedding, user_id, project_id FROM vec_compact_keep WHERE id > ? ORDER BY id LIMIT ?`,
		`SELECT MAX(rowid) FROM vec_chunks_rowids`); err != nil {
		return c, err
	}
	if _, err := exec(`DROP TABLE vec_compact_keep`); err != nil {
		return c, err
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

// copyOutSQL copies the next batch of vectors out of vec_chunks. The batch is
// walked on vec_chunks_rowids, a plain table, so the range is an index seek,
// and each vector is joined in by rowid, which vec0 answers with a point
// lookup even without the partition key. Never rowid IN (…): outside a KNN
// query vec0 answers that with a full scan, so every batch would walk the
// whole table.
const copyOutSQL = `INSERT INTO vec_compact_keep (id, embedding, user_id, project_id)
	SELECT r.rowid, v.embedding, v.user_id, v.project_id
	FROM vec_chunks_rowids r JOIN vec_chunks v ON v.rowid = r.rowid
	WHERE r.rowid > ? ORDER BY r.rowid LIMIT ?`

// copyBatches runs insert, which copies the next compactBatch rows after a
// rowid, until it copies none; last reads the highest rowid copied so far.
func copyBatches(ctx context.Context, tx *sql.Tx, p *compactProgress,
	exec func(string, ...any) (int64, error), insert, last string) error {
	var after int64
	for {
		n, err := exec(insert, after, compactBatch)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		p.add(n)
		var hi sql.NullInt64
		if err := tx.QueryRowContext(ctx, last).Scan(&hi); err != nil {
			return fmt.Errorf("compact vec_chunks: %w", err)
		}
		after = hi.Int64
	}
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

// CompactVectorsAndLog compacts and says so: a line before the rewrite and one
// after, a warning when it failed, nothing when the table was healthy. The
// rewrite takes minutes on a production table, so it announces itself rather
// than leave the boot silent until it is done. A failure is never the
// caller's: the index is complete, bloat only costs time. It reports what it
// found; ok is false when the check itself failed.
func (s *Store) CompactVectorsAndLog(ctx context.Context, args ...any) (c VecCompaction, ok bool) {
	start := time.Now()
	p := &compactProgress{ctx: ctx, args: args}
	stop := heartbeat(ctx, "compacting the vector index, still running", func() []any {
		step, done, total := p.state()
		return append(slices.Clone(args), "step", step, "done", done, "total", total)
	})
	c, err := s.compactVectors(ctx, p)
	stop()
	if err != nil {
		slog.WarnContext(ctx, "compacting the vector index failed", append(args, "err", err)...)
		return c, false
	}
	if c.Compacted {
		slog.InfoContext(ctx, "vector index compacted", append(args, "rows", c.Rows,
			"chunks_before", c.ChunksBefore, "chunks_after", c.ChunksAfter, "took", took(start))...)
	}
	return c, true
}

// CompactVectorsAtBoot compacts when bloated, vacuums after a compaction, and
// otherwise states the table's shape: rows against storage chunks is the one
// number that shows vec0 bloat before searches slow down.
func (s *Store) CompactVectorsAtBoot(ctx context.Context) {
	c, ok := s.CompactVectorsAndLog(ctx, "reason", "boot")
	switch {
	case !ok:
	case c.Compacted:
		s.VacuumAndLog(ctx)
	default:
		slog.InfoContext(ctx, "vector index", "rows", c.Rows, "chunks", c.ChunksBefore)
	}
}

// VacuumAndLog rewrites the database file so the pages a compaction freed go
// back to the disk, and logs the size either side. Compaction alone fixes the
// search time; freed pages are reused, but the file keeps its size until this.
// It rewrites the whole file and blocks writers while it runs, so it belongs
// at boot, before anything else opens a transaction, and says so first.
func (s *Store) VacuumAndLog(ctx context.Context) {
	start := time.Now()
	before, err := s.dbBytes(ctx)
	if err == nil {
		slog.InfoContext(ctx, "vacuuming the database", "bytes_before", before)
		stop := heartbeat(ctx, "vacuuming the database, still running", func() []any { return nil })
		_, err = s.db.ExecContext(ctx, `VACUUM`)
		stop()
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
