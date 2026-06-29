package chat

import (
	"context"
	"strings"
	"testing"
)

// seedThread creates a thread for userID (optionally in projectID) and adds the
// given role/content messages to it, returning the thread id.
func seedThread(t *testing.T, store *Store, userID string, projectID *string, title string, msgs ...struct {
	role    Role
	content string
}) string {
	t.Helper()
	ctx := context.Background()
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: title, ProjectID: projectID})
	if err != nil {
		t.Fatalf("create thread %q: %v", title, err)
	}
	for _, m := range msgs {
		if _, err := store.AddMessage(ctx, userID, thread.ID, m.role, m.content); err != nil {
			t.Fatalf("add message to %q: %v", title, err)
		}
	}
	return thread.ID
}

type msg = struct {
	role    Role
	content string
}

func TestSearchMessages_MatchesAndRanks(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	frozen := seedThread(t, store, alice, nil, "Sharing design",
		msg{RoleUser, "Why did we choose frozen snapshots for sharing?"},
		msg{RoleAssistant, "We froze the snapshot so later edits never leak into a shared link."},
	)
	seedThread(t, store, alice, nil, "Unrelated",
		msg{RoleUser, "What is the weather like today?"},
		msg{RoleAssistant, "I have no idea about the weather."},
	)

	hits, err := store.SearchMessages(ctx, alice, "frozen snapshots", nil, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected hits for 'frozen snapshots', got none")
	}
	for _, h := range hits {
		if h.ThreadID != frozen {
			t.Fatalf("hit from unexpected thread %s (title %q)", h.ThreadID, h.ThreadTitle)
		}
		if h.ThreadTitle != "Sharing design" {
			t.Fatalf("hit carried wrong thread title %q", h.ThreadTitle)
		}
	}
}

func TestSearchMessages_SnippetCentersOnMatchDeepInMessage(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// A long message whose only match is near the very end — a head-truncated
	// excerpt would miss it entirely, so this asserts snippet() is match-centered.
	filler := strings.Repeat("lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 60)
	seedThread(t, store, alice, nil, "Long answer",
		msg{RoleAssistant, filler + "Finally, the capybara is the decisive detail."},
	)

	hits, err := store.SearchMessages(ctx, alice, "capybara", nil, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if !strings.Contains(hits[0].Snippet, "capybara") {
		t.Fatalf("snippet should contain the deep match term, got: %q", hits[0].Snippet)
	}
	if strings.Contains(hits[0].Snippet, "«capybara»") == false {
		t.Fatalf("snippet should highlight the match with « », got: %q", hits[0].Snippet)
	}
}

func TestSearchMessages_IsUserScoped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	bob := insertTestUser(t, db, "bob")
	store := NewStore(db)

	seedThread(t, store, alice, nil, "Alice secret",
		msg{RoleUser, "The passphrase is sphinx."},
	)

	hits, err := store.SearchMessages(ctx, bob, "sphinx", nil, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("bob must not see alice's messages, got %d hits", len(hits))
	}
}

func TestSearchMessages_ExcludesToolRole(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	seedThread(t, store, alice, nil, "Tool noise",
		msg{RoleUser, "go ahead"},
		msg{RoleTool, "pipeline executed widget successfully"},
	)

	hits, err := store.SearchMessages(ctx, alice, "widget", nil, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("tool-role messages must not be indexed, got %d hits", len(hits))
	}
}

func TestSearchMessages_ProjectFilterAndExcludeThread(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, alice, CreateProjectInput{Name: "Regs"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	inProject := seedThread(t, store, alice, &project.ID, "In project",
		msg{RoleUser, "FINMA requires capital thresholds."},
	)
	seedThread(t, store, alice, nil, "Outside project",
		msg{RoleUser, "FINMA also appears here outside the project."},
	)

	// Scoped to the project: only the in-project thread matches.
	scoped, err := store.SearchMessages(ctx, alice, "FINMA", &project.ID, "", 10)
	if err != nil {
		t.Fatalf("scoped search: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ThreadID != inProject {
		t.Fatalf("project filter wrong: %+v", scoped)
	}
	if scoped[0].ProjectID == nil || *scoped[0].ProjectID != project.ID {
		t.Fatalf("hit should carry project id %s, got %+v", project.ID, scoped[0].ProjectID)
	}

	// Excluding the in-project thread drops it from the unscoped results.
	excluded, err := store.SearchMessages(ctx, alice, "FINMA", nil, inProject, 10)
	if err != nil {
		t.Fatalf("excluded search: %v", err)
	}
	for _, h := range excluded {
		if h.ThreadID == inProject {
			t.Fatalf("excluded thread %s still present", inProject)
		}
	}
}

func TestSearchMessages_ExcludesArchivedThreads(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	archived := seedThread(t, store, alice, nil, "Archived",
		msg{RoleUser, "zebra crossing notes"},
	)
	if _, err := store.SetThreadArchived(ctx, alice, archived, true); err != nil {
		t.Fatalf("archive thread: %v", err)
	}

	hits, err := store.SearchMessages(ctx, alice, "zebra", nil, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("archived threads must be excluded, got %d hits", len(hits))
	}
}

func TestSearchMessages_DeleteKeepsIndexInSync(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread := seedThread(t, store, alice, nil, "Doomed",
		msg{RoleUser, "quokka sighting confirmed"},
	)
	if hits, _ := store.SearchMessages(ctx, alice, "quokka", nil, "", 10); len(hits) != 1 {
		t.Fatalf("expected 1 hit before delete, got %d", len(hits))
	}
	if _, err := store.DeleteThread(ctx, alice, thread); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	hits, err := store.SearchMessages(ctx, alice, "quokka", nil, "", 10)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("deleting a thread must drop its messages from the index, got %d hits", len(hits))
	}
}

func TestSearchMessages_QuerySanitizationNeverErrors(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	store := NewStore(db)

	seedThread(t, store, alice, nil, "Punctuation",
		msg{RoleUser, "the SST threshold is the key number"},
	)

	// Raw FTS5 metacharacters/operators would raise a syntax error if passed
	// through unsanitized; buildFTSMatchQuery must neutralize them.
	for _, q := range []string{`"unterminated`, `AND OR NOT`, `SST*`, `(threshold)`, `a:b`, `   `, `:::`, `***`} {
		if _, err := store.SearchMessages(ctx, alice, q, nil, "", 10); err != nil {
			t.Fatalf("query %q must not error, got %v", q, err)
		}
	}
}

// Note: the migration's backfill clause is exercised faithfully — against the
// real 0021 file bytes applied to a populated DB — by
// TestMigration0021_BackfillsPopulatedCorpus in the store package.

func TestBuildFTSMatchQuery(t *testing.T) {
	cases := map[string]string{
		"frozen snapshots": `"frozen" "snapshots"`,
		`AND`:              `"AND"`,
		`a"b`:              `"a""b"`,
		"":                 "",
		"   ":              "",
	}
	for in, want := range cases {
		if got := buildFTSMatchQuery(in); got != want {
			t.Fatalf("buildFTSMatchQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
