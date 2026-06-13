package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

type testCitation struct {
	DocumentID string  `json:"documentId"`
	Filename   string  `json:"filename"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

func TestStore_scrubOutOfScopeMessageCitations(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	seedCitationScrubData(t, db)

	if err := s.ScrubOutOfScopeMessageCitations(ctx); err != nil {
		t.Fatalf("ScrubOutOfScopeMessageCitations: %v", err)
	}

	assertMessageCitationIDs(t, db, "m_thread", []string{"d_thread_1", "d_global"})
	assertMessageCitationIDs(t, db, "m_project", []string{"d_project_1", "d_global"})
	assertMessageCitationIDs(t, db, "m_empty", []string{})
}

func TestStore_scrubOutOfScopeMessageCitationsIsOneTime(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	seedCitationScrubData(t, db)

	if err := s.ScrubOutOfScopeMessageCitations(ctx); err != nil {
		t.Fatalf("first scrub: %v", err)
	}
	if _, err := db.Exec(`UPDATE messages SET citations = ? WHERE id = 'm_empty'`, citationsJSON(t, "d_thread_1")); err != nil {
		t.Fatalf("re-seed stale citation after marker: %v", err)
	}
	if err := s.ScrubOutOfScopeMessageCitations(ctx); err != nil {
		t.Fatalf("second scrub: %v", err)
	}

	assertMessageCitationIDs(t, db, "m_empty", []string{"d_thread_1"})
	var markerCount int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, scrubCitationsMarker).Scan(&markerCount); err != nil {
		t.Fatalf("count scrub marker: %v", err)
	}
	if markerCount != 1 {
		t.Fatalf("scrub marker count = %d, want 1", markerCount)
	}
}

func seedCitationScrubData(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO projects (id, user_id, name) VALUES ('p2','u1','Project 2')`); err != nil {
		t.Fatalf("seed p2: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO threads (id, user_id, project_id, title) VALUES
			('t1','u1',NULL,'Thread 1'),
			('t2','u1',NULL,'Thread 2'),
			('tp1','u1','p1','Project 1 Thread'),
			('tp2','u1','p2','Project 2 Thread')`); err != nil {
		t.Fatalf("seed threads: %v", err)
	}

	p1, p2, t1, t2 := "p1", "p2", "t1", "t2"
	docs := []Document{
		{ID: "d_global", UserID: "u1", VolumeRelpath: "files/global.txt", Filename: "global.txt", MIME: "text/plain", Status: StatusEmbedded},
		{ID: "d_thread_1", UserID: "u1", ThreadID: &t1, VolumeRelpath: "files/thread1.txt", Filename: "thread1.txt", MIME: "text/plain", Status: StatusEmbedded},
		{ID: "d_thread_2", UserID: "u1", ThreadID: &t2, VolumeRelpath: "files/thread2.txt", Filename: "thread2.txt", MIME: "text/plain", Status: StatusEmbedded},
		{ID: "d_project_1", UserID: "u1", ProjectID: &p1, VolumeRelpath: "projects/p1/project1.txt", Filename: "project1.txt", MIME: "text/plain", Status: StatusEmbedded},
		{ID: "d_project_2", UserID: "u1", ProjectID: &p2, VolumeRelpath: "projects/p2/project2.txt", Filename: "project2.txt", MIME: "text/plain", Status: StatusEmbedded},
	}
	s := NewStore(db)
	for _, doc := range docs {
		if err := s.CreateDocument(context.Background(), doc); err != nil {
			t.Fatalf("seed document %s: %v", doc.ID, err)
		}
	}

	seedMessage := func(id, threadID string, docIDs ...string) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO messages (id, thread_id, user_id, role, content, citations)
			VALUES (?, ?, 'u1', 'assistant', 'answer', ?)`,
			id, threadID, citationsJSON(t, docIDs...)); err != nil {
			t.Fatalf("seed message %s: %v", id, err)
		}
	}
	seedMessage("m_thread", "t1", "d_thread_2", "d_thread_1", "d_global", "d_missing")
	seedMessage("m_project", "tp1", "d_project_2", "d_project_1", "d_global", "d_thread_1")
	seedMessage("m_empty", "t2", "d_thread_1", "d_missing")
}

func citationsJSON(t *testing.T, docIDs ...string) string {
	t.Helper()
	citations := make([]testCitation, 0, len(docIDs))
	for _, id := range docIDs {
		citations = append(citations, testCitation{
			DocumentID: id,
			Filename:   id + ".txt",
			Snippet:    "snippet",
			Score:      0.9,
		})
	}
	raw, err := json.Marshal(citations)
	if err != nil {
		t.Fatalf("marshal citations: %v", err)
	}
	return string(raw)
}

func assertMessageCitationIDs(t *testing.T, db *sql.DB, messageID string, want []string) {
	t.Helper()
	var raw string
	if err := db.QueryRow(`SELECT citations FROM messages WHERE id = ?`, messageID).Scan(&raw); err != nil {
		t.Fatalf("load message %s citations: %v", messageID, err)
	}
	var citations []testCitation
	if err := json.Unmarshal([]byte(raw), &citations); err != nil {
		t.Fatalf("unmarshal message %s citations %q: %v", messageID, raw, err)
	}
	got := make([]string, 0, len(citations))
	for _, citation := range citations {
		got = append(got, citation.DocumentID)
	}
	if len(got) != len(want) {
		t.Fatalf("message %s citations = %v, want %v", messageID, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("message %s citations = %v, want %v", messageID, got, want)
		}
	}
}
