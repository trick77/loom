package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// queryPlan renders EXPLAIN QUERY PLAN for one statement.
func queryPlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, detail)
	}
	return strings.Join(lines, "\n")
}

func TestThreadListOrdersOffTheRecencyIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	plan := queryPlan(t, db, `
SELECT id FROM threads
WHERE user_id = ? AND archived_at IS NULL
ORDER BY COALESCE(last_message_at, updated_at) DESC, updated_at DESC, id DESC
LIMIT 30`, "u")
	if !strings.Contains(plan, "idx_threads_user_recency") {
		t.Fatalf("thread list does not use the recency index:\n%s", plan)
	}
	if strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("thread list still sorts in a temporary b-tree:\n%s", plan)
	}
}

func TestProjectArtifactListUsesTheProjectIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	plan := queryPlan(t, db, `
SELECT id FROM artifacts
WHERE user_id = ? AND project_id = ? AND deleted_at IS NULL
ORDER BY created_at ASC`, "u", "p")
	if !strings.Contains(plan, "idx_artifacts_user_project") {
		t.Fatalf("project artifact list does not use the project index:\n%s", plan)
	}
}

func openPlanDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// A thread's transcript is read on every send and every thread view; it must
// seek on (user, thread) and come back in rowid order without a sort.
func TestThreadMessagesSeekOnUserAndThread(t *testing.T) {
	db := openPlanDB(t)
	plan := queryPlan(t, db, `
SELECT id FROM messages
WHERE user_id = ? AND thread_id = ?
ORDER BY rowid ASC`, "u", "t")
	if !strings.Contains(plan, "idx_messages_user_thread") {
		t.Fatalf("thread messages do not use the (user, thread) index:\n%s", plan)
	}
	if strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("thread messages still sort in a temporary b-tree:\n%s", plan)
	}
}

func TestDocumentChunksSeekOnUserAndDocument(t *testing.T) {
	db := openPlanDB(t)
	for name, query := range map[string]string{
		"join": `
SELECT d.id, COALESCE(SUM(c.token_count), 0)
FROM documents d
LEFT JOIN chunks c ON c.document_id = d.id AND c.user_id = d.user_id
WHERE d.user_id = ? AND d.status = 'embedded'
GROUP BY d.id`,
		"by document": `SELECT id FROM chunks WHERE user_id = ? AND document_id = 'd'`,
	} {
		plan := queryPlan(t, db, query, "u")
		if !strings.Contains(plan, "idx_chunks_user_document (user_id=? AND document_id=?") {
			t.Fatalf("%s: chunks are not sought by (user, document):\n%s", name, plan)
		}
	}
}

// The documents.artifact_id ON DELETE SET NULL action looks rows up by
// artifact_id alone; without an index leading on it every artifact delete scans.
func TestDocumentsByArtifactUsesAnIndexSeek(t *testing.T) {
	db := openPlanDB(t)
	plan := queryPlan(t, db, `SELECT id FROM documents WHERE artifact_id = ?`, "a")
	if !strings.Contains(plan, "SEARCH") || !strings.Contains(plan, "idx_documents_artifact") {
		t.Fatalf("documents by artifact is not an index seek:\n%s", plan)
	}
}

// Indexes a later one fully supersedes only cost writes.
func TestSupersededIndexesAreDropped(t *testing.T) {
	db := openPlanDB(t)
	for _, name := range []string{
		"idx_threads_user_recent", "idx_threads_user_starred", "idx_chunks_user",
	} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		if n != 0 {
			t.Fatalf("index %s still exists", name)
		}
	}
	plan := queryPlan(t, db, `
SELECT id FROM threads
WHERE user_id = ? AND archived_at IS NULL AND starred = 1
ORDER BY COALESCE(last_message_at, updated_at) DESC, updated_at DESC, id DESC
LIMIT 30`, "u")
	if !strings.Contains(plan, "idx_threads_user_recency") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("starred list does not come off the recency index:\n%s", plan)
	}
}
