package chat

import (
	"context"
	"database/sql"
	"testing"
)

// oldActivity is a sentinel far in the past, written directly so a subsequent
// datetime('now') bump is unambiguously newer (SQLite timestamps are second-
// granular, so comparing against "now" in the same second would be flaky).
const oldActivity = "2000-01-01 00:00:00"

func setLastActivity(t *testing.T, db *sql.DB, projectID, value string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE projects SET last_activity_at = ? WHERE id = ?`, value, projectID); err != nil {
		t.Fatalf("set last_activity_at: %v", err)
	}
}

func lastActivityRaw(t *testing.T, db *sql.DB, projectID string) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(context.Background(),
		`SELECT last_activity_at FROM projects WHERE id = ?`, projectID).Scan(&v); err != nil {
		t.Fatalf("read last_activity_at: %v", err)
	}
	return v
}

func TestStore_LastActivityAt_AdvancesOnThreadAndMessage(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if project.LastActivityAt.IsZero() {
		t.Fatal("CreateProject() left last_activity_at zero, want it set")
	}

	// Creating a thread in the project is activity.
	setLastActivity(t, db, project.ID, oldActivity)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{ProjectID: &project.ID, Title: "Planning"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if got := lastActivityRaw(t, db, project.ID); got == oldActivity {
		t.Fatal("CreateThread() did not bump project last_activity_at")
	}

	// Adding a message to a thread in the project is activity.
	setLastActivity(t, db, project.ID, oldActivity)
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "hello"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}
	if got := lastActivityRaw(t, db, project.ID); got == oldActivity {
		t.Fatal("AddMessage() did not bump project last_activity_at")
	}
}

func TestStore_LastActivityAt_MessageInProjectlessThreadTouchesNoProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	setLastActivity(t, db, project.ID, oldActivity)

	// A thread with no project: its messages must not touch any project row.
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Loose"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "hello"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}
	if got := lastActivityRaw(t, db, project.ID); got != oldActivity {
		t.Fatalf("project last_activity_at changed to %q, want untouched %q", got, oldActivity)
	}
}

func TestStore_LastActivityAt_MovedByUpdateProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	setLastActivity(t, db, project.ID, oldActivity)

	newName := "Renamed"
	if _, _, err := store.UpdateProject(ctx, userID, project.ID, UpdateProjectInput{Name: &newName}); err != nil {
		t.Fatalf("UpdateProject() error: %v", err)
	}
	if got := lastActivityRaw(t, db, project.ID); got == oldActivity {
		t.Fatal("UpdateProject() did not bump project last_activity_at")
	}
}

// Star, archive, and auto-description are incidental (non-user-activity) events:
// they must never move last_activity_at, the field the card and "Recent activity"
// sort read. This is the regression guard for the original bug.
func TestStore_LastActivityAt_NotMovedByIncidentalEvents(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	t.Run("star", func(t *testing.T) {
		project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Star"})
		if err != nil {
			t.Fatalf("CreateProject() error: %v", err)
		}
		setLastActivity(t, db, project.ID, oldActivity)
		if _, _, err := store.SetProjectStarred(ctx, userID, project.ID, true); err != nil {
			t.Fatalf("SetProjectStarred() error: %v", err)
		}
		if got := lastActivityRaw(t, db, project.ID); got != oldActivity {
			t.Fatalf("star moved last_activity_at to %q, want %q", got, oldActivity)
		}
	})

	t.Run("archive", func(t *testing.T) {
		project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Archive"})
		if err != nil {
			t.Fatalf("CreateProject() error: %v", err)
		}
		setLastActivity(t, db, project.ID, oldActivity)
		if _, err := store.SetProjectArchived(ctx, userID, project.ID, true); err != nil {
			t.Fatalf("SetProjectArchived() error: %v", err)
		}
		if got := lastActivityRaw(t, db, project.ID); got != oldActivity {
			t.Fatalf("archive moved last_activity_at to %q, want %q", got, oldActivity)
		}
	})

	t.Run("auto-description", func(t *testing.T) {
		project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Describe"})
		if err != nil {
			t.Fatalf("CreateProject() error: %v", err)
		}
		setLastActivity(t, db, project.ID, oldActivity)
		if _, _, err := store.SetAutoProjectDescription(ctx, userID, project.ID, "auto generated summary", 1); err != nil {
			t.Fatalf("SetAutoProjectDescription() error: %v", err)
		}
		if got := lastActivityRaw(t, db, project.ID); got != oldActivity {
			t.Fatalf("auto-description moved last_activity_at to %q, want %q", got, oldActivity)
		}
	})
}

func TestStore_ListProjects_OrdersByRecentActivity(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	older, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Older"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	newer, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Newer"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	// Pin a deterministic baseline: "newer" most recent.
	setLastActivity(t, db, older.ID, "2000-01-01 00:00:00")
	setLastActivity(t, db, newer.ID, "2001-01-01 00:00:00")

	// A new message in "older" must lift it above "newer".
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{ProjectID: &older.ID, Title: "T"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "ping"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}

	projects, err := store.ListProjects(ctx, userID, false)
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("ListProjects() returned %d projects, want 2", len(projects))
	}
	if projects[0].ID != older.ID {
		t.Fatalf("ListProjects()[0] = %q, want %q (most recent activity first)", projects[0].ID, older.ID)
	}
}
