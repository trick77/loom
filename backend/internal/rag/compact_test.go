package rag

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// logCapture records every log line emitted while it is the default handler.
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(string) slog.Handler      { return c }

func (c *logCapture) find(msg string) (slog.Record, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

func (c *logCapture) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.records)
}

func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	c := &logCapture{}
	restore := slog.Default()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() { slog.SetDefault(restore) })
	return c
}

func attrsOf(r slog.Record) map[string]string {
	out := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.String()
		return true
	})
	return out
}

const compactDim = 4

func compactVec(marker float32) []float32 {
	v := make([]float32, compactDim)
	for i := range v {
		v[i] = marker
	}
	v[0] = 1
	return v
}

// churnVectors inserts n vectors into user u1 (project "p1") and deletes all
// but the last keep, the way re-indexing does: vec0 clears a deleted row's
// validity bit and never frees its chunk, and inserts only append to the
// partition's newest chunk. u2 gets one vector, so a second partition exists.
func churnVectors(t *testing.T, s *Store, n, keep int) {
	t.Helper()
	ctx := context.Background()
	if err := s.RebuildVectorTable(ctx, compactDim); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for i := 1; i <= n; i++ {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (?, ?, 'u1', 'p1')`,
			i, vecLiteral(compactVec(float32(i)))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (?, ?, 'u2', '')`,
		n+1, vecLiteral(compactVec(7))); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vec_chunks WHERE rowid <= ?`, n-keep); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

type neighbour struct {
	id      int64
	project string
}

func nearestFor(t *testing.T, s *Store, user string, marker float32, k int) []neighbour {
	t.Helper()
	rows, err := s.db.Query(`SELECT rowid, project_id FROM vec_chunks
		WHERE embedding MATCH ? AND k = ? AND user_id = ? ORDER BY distance`,
		vecLiteral(compactVec(marker)), k, user)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []neighbour
	for rows.Next() {
		var n neighbour
		if err := rows.Scan(&n.id, &n.project); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func countRows(t *testing.T, s *Store, q string) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCompactVectors_dropsTheChunksDeletesLeftBehind(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 5000, 100)
	beforeU1 := nearestFor(t, s, "u1", 4950, 10)
	beforeU2 := nearestFor(t, s, "u2", 7, 1)

	got, err := s.CompactVectors(context.Background())
	if err != nil {
		t.Fatalf("CompactVectors() err = %v", err)
	}

	// u1: 5 chunks for 100 rows, u2: 1 chunk for 1 row. Needed: 1 + 1.
	want := VecCompaction{Compacted: true, Rows: 101, ChunksBefore: 6, ChunksAfter: 2}
	if got != want {
		t.Errorf("CompactVectors() = %+v, want %+v", got, want)
	}
	// Same rowids, same vectors, same partition and metadata: retrieval
	// filters on user_id and project_id and joins chunks on rowid.
	if after := nearestFor(t, s, "u1", 4950, 10); !slices.Equal(after, beforeU1) {
		t.Errorf("u1 nearest after compaction = %v, want %v", after, beforeU1)
	}
	if after := nearestFor(t, s, "u2", 7, 1); !slices.Equal(after, beforeU2) {
		t.Errorf("u2 nearest after compaction = %v, want %v", after, beforeU2)
	}
	if w, err := s.VectorWidth(context.Background()); err != nil || w != compactDim {
		t.Errorf("VectorWidth() = %d, %v, want %d", w, err, compactDim)
	}
}

func TestCompactVectors_leavesAHealthyTableAlone(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 1500, 1400)

	got, err := s.CompactVectors(context.Background())
	if err != nil {
		t.Fatalf("CompactVectors() err = %v", err)
	}
	if got.Compacted || got.ChunksBefore != 3 || got.ChunksAfter != 3 || got.Rows != 1401 {
		t.Errorf("CompactVectors() = %+v, want untouched with 3 chunks and 1401 rows", got)
	}
}

func TestCompactVectors_anEmptyTableIsNotBloated(t *testing.T) {
	s, _ := newTestStore(t)
	got, err := s.CompactVectors(context.Background())
	if err != nil || got.Compacted || got.Rows != 0 {
		t.Errorf("CompactVectors() = %+v, %v, want untouched", got, err)
	}
}

func TestCompactVectors_aFailureLeavesTheTableAsItWas(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 5000, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.CompactVectors(ctx); err == nil {
		t.Fatal("CompactVectors() on a cancelled context succeeded")
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM vec_chunks_chunks`); n != 6 {
		t.Errorf("chunks = %d, want 6 untouched", n)
	}
}

func TestCompactVectorsAtBoot_compactsAndVacuumsABloatedTable(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 5000, 100)
	logs := captureLogs(t)

	s.CompactVectorsAtBoot(context.Background())

	r, ok := logs.find("vector index compacted")
	if !ok {
		t.Fatalf("no compaction line, records = %v", logs.records)
	}
	a := attrsOf(r)
	for k, v := range map[string]string{"rows": "101", "chunks_before": "6", "chunks_after": "2"} {
		if a[k] != v {
			t.Errorf("attr %s = %q, want %q (all: %v)", k, a[k], v, a)
		}
	}
	if a["took"] == "" {
		t.Errorf("no took attr: %v", a)
	}
	v, ok := logs.find("database vacuumed")
	if !ok {
		t.Fatalf("no vacuum line, records = %v", logs.records)
	}
	var before, after int64
	v.Attrs(func(at slog.Attr) bool {
		switch at.Key {
		case "bytes_before":
			before = at.Value.Int64()
		case "bytes_after":
			after = at.Value.Int64()
		}
		return true
	})
	if after <= 0 || after >= before {
		t.Errorf("bytes_before = %d, bytes_after = %d, want it to shrink", before, after)
	}
}

// Both rewrites take minutes on a production table and hold the boot before
// the listener opens, so each says it started, not only that it finished.
func TestCompactVectorsAtBoot_saysEachRewriteBeforeItRuns(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 5000, 100)
	logs := captureLogs(t)

	s.CompactVectorsAtBoot(context.Background())

	logs.mu.Lock()
	records := slices.Clone(logs.records)
	logs.mu.Unlock()
	var msgs []string
	for _, r := range records {
		msgs = append(msgs, r.Message)
	}
	want := []string{"compacting the vector index", "copying vectors out", "writing vectors back",
		"vector index compacted", "vacuuming the database", "database vacuumed"}
	if !slices.Equal(msgs, want) {
		t.Fatalf("messages = %q, want %q", msgs, want)
	}
	a := attrsOf(records[0])
	for k, v := range map[string]string{"reason": "boot", "rows": "101", "chunks": "6", "chunks_needed": "2"} {
		if a[k] != v {
			t.Errorf("attr %s = %q, want %q (all: %v)", k, a[k], v, a)
		}
	}
	var before int64
	records[4].Attrs(func(at slog.Attr) bool {
		if at.Key == "bytes_before" {
			before = at.Value.Int64()
		}
		return true
	})
	if before <= 0 {
		t.Errorf("vacuum start line has no bytes_before: %v", records[4])
	}
}

// vec0 answers rowid IN (…) outside a KNN query with a full scan (plan
// "…:1"), which made rongo's batched copy walk the whole table per batch. The
// copy-out has to read each vector with a point lookup (plan "…:2").
func TestCompactVectors_copyOutReadsEachVectorByPointLookup(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 10, 10)
	if _, err := s.db.Exec(`CREATE TABLE vec_compact_keep (id INTEGER PRIMARY KEY, embedding BLOB NOT NULL, user_id TEXT NOT NULL, project_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN `+copyOutSQL, 0, compactBatch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	point := false
	for _, d := range plan {
		if strings.Contains(d, "VIRTUAL TABLE INDEX") {
			point = strings.Contains(d, ":2")
		}
	}
	if !point {
		t.Errorf("copy-out plan = %q, want a vec0 point lookup (INDEX n:2…)", plan)
	}
}

// A copy longer than one heartbeat says where it has got to while it runs.
func TestCompactVectorsAndLog_heartbeatReportsProgress(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 6000, 1000)
	defer func(d time.Duration, b int) { compactHeartbeat, compactBatch = d, b }(compactHeartbeat, compactBatch)
	compactHeartbeat, compactBatch = time.Millisecond, 64
	beforeU1 := nearestFor(t, s, "u1", 5999, 3)
	beforeU2 := nearestFor(t, s, "u2", 7, 1)
	logs := captureLogs(t)

	s.CompactVectorsAndLog(context.Background(), "reason", "boot")

	// The last heartbeat: the first may fire while the table is still being
	// measured, before any step has begun.
	var a map[string]string
	logs.mu.Lock()
	for _, r := range logs.records {
		if r.Message == "compacting the vector index, still running" {
			a = attrsOf(r)
		}
	}
	logs.mu.Unlock()
	if a == nil {
		t.Fatalf("no heartbeat, records = %v", logs.records)
	}
	for _, k := range []string{"reason", "step", "done", "total", "elapsed"} {
		if a[k] == "" {
			t.Errorf("heartbeat lacks %s: %v", k, a)
		}
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM vec_chunks_rowids`); n != 1001 {
		t.Errorf("rows = %d, want 1001", n)
	}
	// Batches carry the partition key and the metadata with every vector.
	if got := nearestFor(t, s, "u1", 5999, 3); !slices.Equal(got, beforeU1) {
		t.Errorf("u1 nearest = %v, want %v", got, beforeU1)
	}
	if got := nearestFor(t, s, "u2", 7, 1); !slices.Equal(got, beforeU2) {
		t.Errorf("u2 nearest = %v, want %v", got, beforeU2)
	}
}

func TestCompactVectorsAtBoot_statesAHealthyTable(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 10, 10)
	logs := captureLogs(t)

	s.CompactVectorsAtBoot(context.Background())

	r, ok := logs.find("vector index")
	if !ok {
		t.Fatalf("no vector index line, records = %v", logs.records)
	}
	if a := attrsOf(r); a["rows"] != "11" || a["chunks"] != "2" {
		t.Errorf("attrs = %v, want rows=11 chunks=2", a)
	}
	if _, ok := logs.find("database vacuumed"); ok {
		t.Error("a healthy table was vacuumed")
	}
}

func TestCompactVectorsAtBoot_aFailedCheckOnlyWarns(t *testing.T) {
	s, _ := newTestStore(t)
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.CompactVectorsAtBoot(ctx)

	r, ok := logs.find("compacting the vector index failed")
	if !ok || r.Level != slog.LevelWarn || logs.len() != 1 {
		t.Errorf("records = %v, want one warning", logs.records)
	}
}

func TestWarnVectorBloat_warnsWithoutTouchingTheTable(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 5000, 100)
	logs := captureLogs(t)

	s.WarnVectorBloat(context.Background(), "document", "d1")

	r, ok := logs.find("vector index bloated, compacted at next boot")
	if !ok || r.Level != slog.LevelWarn {
		t.Fatalf("want a bloat warning, records = %v", logs.records)
	}
	a := attrsOf(r)
	for k, v := range map[string]string{"document": "d1", "rows": "101", "chunks": "6", "chunks_needed": "2"} {
		if a[k] != v {
			t.Errorf("attr %s = %q, want %q (all: %v)", k, a[k], v, a)
		}
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM vec_chunks_chunks`); n != 6 {
		t.Errorf("chunks = %d, want 6 untouched", n)
	}
}

func TestWarnVectorBloat_quietWhenHealthy(t *testing.T) {
	s, _ := newTestStore(t)
	churnVectors(t, s, 10, 10)
	logs := captureLogs(t)

	s.WarnVectorBloat(context.Background())

	if logs.len() != 0 {
		t.Errorf("records = %v, want none", logs.records)
	}
}

func TestWarnVectorBloat_aFailedMeasureWarns(t *testing.T) {
	s, _ := newTestStore(t)
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.WarnVectorBloat(ctx)

	if _, ok := logs.find("measuring the vector index failed"); !ok {
		t.Errorf("records = %v, want the failure", logs.records)
	}
}
