package store

import (
	"path/filepath"
	"testing"
)

func TestSqliteVec_versionAndKNN(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vec.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	// vec_version() proves the extension is linked.
	var ver string
	if err := db.QueryRow(`SELECT vec_version()`).Scan(&ver); err != nil {
		t.Fatalf("vec_version() error: %v", err)
	}
	if ver == "" {
		t.Fatal("vec_version() returned empty string")
	}

	// Create a vec0 table, insert 3 vectors, run a KNN query.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE v USING vec0(embedding float[3])`); err != nil {
		t.Fatalf("create vec0: %v", err)
	}
	rows := [][2]any{
		{int64(1), "[1.0, 0.0, 0.0]"},
		{int64(2), "[0.0, 1.0, 0.0]"},
		{int64(3), "[0.9, 0.1, 0.0]"},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO v(rowid, embedding) VALUES (?, ?)`, r[0], r[1]); err != nil {
			t.Fatalf("insert vector: %v", err)
		}
	}

	var nearest int64
	err = db.QueryRow(`
		SELECT rowid FROM v
		WHERE embedding MATCH ? AND k = 1
		ORDER BY distance`, "[1.0, 0.0, 0.0]").Scan(&nearest)
	if err != nil {
		t.Fatalf("KNN query: %v", err)
	}
	if nearest != 1 {
		t.Errorf("nearest rowid = %d, want 1", nearest)
	}
}
