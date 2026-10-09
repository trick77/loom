package documents

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/store"
)

type bloatLogs struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *bloatLogs) Enabled(context.Context, slog.Level) bool { return true }
func (c *bloatLogs) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *bloatLogs) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *bloatLogs) WithGroup(string) slog.Handler      { return c }

// bloatWarning returns the attributes of the bloat warning, if one was logged.
func (c *bloatLogs) bloatWarning() (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if r.Message == "vector index bloated, compacted at next boot" {
			a := map[string]string{}
			r.Attrs(func(at slog.Attr) bool {
				a[at.Key] = at.Value.String()
				return true
			})
			return a, true
		}
	}
	return nil, false
}

// bloatedService is a Service over a vec_chunks that re-indexing has left with
// far more storage chunks than its live rows need: vec0 never frees a deleted
// vector's chunk. The rag store is returned too, so a test can give a document
// real vectors (the fake indexer writes none).
func bloatedService(t *testing.T) (*Service, *rag.Store) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`,
		`INSERT INTO threads (id, user_id, title) VALUES ('thread_1', 'u', 'Thread')`,
		`INSERT INTO projects (id, user_id, name) VALUES ('p1', 'u', 'Project')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rs := rag.NewStore(db)
	if err := rs.RebuildVectorTable(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5000; i++ {
		if _, err := tx.Exec(`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (?, ?, 'u', '')`,
			1_000_000+i, fmt.Sprintf("[%d,0,0,0]", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM vec_chunks WHERE rowid <= ?`, 1_000_000+4900); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return NewService(rs, artifact.NewStore(db), &fakeIndexer{}, fakeEmbedder{}, filepath.Join(dir, "users")), rs
}

func TestService_vectorDeletesWarnOfBloat(t *testing.T) {
	// Every path that deletes vectors can leave vec_chunks bloated, and the
	// rebuild only runs at boot: each says so, naming what it deleted. (A
	// re-index replaces vectors inside rag.Store.ReplaceChunks, tested there.)
	projectID := "p1"
	for _, tc := range []struct {
		name   string
		upload UploadInput
		run    func(*Service, string) error
		want   map[string]string
	}{
		{"delete", UploadInput{}, func(s *Service, id string) error { return s.Delete(context.Background(), "u", id) },
			map[string]string{"user": "u", "document": "<doc>"}},
		{"thread", UploadInput{ThreadID: "thread_1"}, func(s *Service, _ string) error { return s.DeleteThreadData(context.Background(), "u", "thread_1") },
			map[string]string{"user": "u", "thread": "thread_1"}},
		{"project", UploadInput{ProjectID: &projectID}, func(s *Service, _ string) error { return s.DeleteProjectData(context.Background(), "u", "p1") },
			map[string]string{"user": "u", "project": "p1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, rs := bloatedService(t)
			in := tc.upload
			in.UserID, in.Filename, in.Reader = "u", "a.txt", strings.NewReader("a")
			doc, _, err := svc.Upload(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if err := rs.ReplaceChunks(context.Background(), "u", doc.ID, []rag.TextChunk{{Text: "a"}}, [][]float32{{1, 0, 0, 0}}); err != nil {
				t.Fatalf("seed vectors: %v", err)
			}
			logs := &bloatLogs{}
			restore := slog.Default()
			slog.SetDefault(slog.New(logs))
			t.Cleanup(func() { slog.SetDefault(restore) })

			if err := tc.run(svc, doc.ID); err != nil {
				t.Fatalf("run: %v", err)
			}

			got, ok := logs.bloatWarning()
			if !ok {
				t.Fatalf("no bloat warning, records = %v", logs.records)
			}
			for k, v := range tc.want {
				if v == "<doc>" {
					v = doc.ID
				}
				if got[k] != v {
					t.Errorf("attr %s = %q, want %q (all: %v)", k, got[k], v, got)
				}
			}
		})
	}
}

// A delete that removed no vector has nothing to report and must not pay for
// the whole-table measurement: deleting a chat without attachments is the
// common case.
func TestService_deleteWithoutVectorsSkipsTheBloatCheck(t *testing.T) {
	svc, _ := bloatedService(t)
	logs := &bloatLogs{}
	restore := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(restore) })

	if err := svc.DeleteThreadData(context.Background(), "u", "thread_1"); err != nil {
		t.Fatal(err)
	}

	if _, ok := logs.bloatWarning(); ok {
		t.Fatalf("bloat warning for a delete that removed nothing, records = %v", logs.records)
	}
}
