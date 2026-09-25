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
