package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/store"
)

func openTestDB(t *testing.T) DBTX {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUserStore_UpsertFromClaimsCreatesAndRefreshesRole(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)

	claims := Claims{
		Subject:  "authentik-sub-1",
		Username: "jan",
		Email:    "jan@example.com",
		Name:     "Jan",
		Groups:   []string{"loom-admins"},
	}

	user, err := store.UpsertFromClaims(context.Background(), claims, "loom-admins")
	if err != nil {
		t.Fatalf("UpsertFromClaims() error: %v", err)
	}
	if user.Role != RoleAdmin {
		t.Fatalf("role = %q, want admin", user.Role)
	}
	if user.ID == "" {
		t.Fatal("user id is empty")
	}

	claims.Groups = []string{"family"}
	user, err = store.UpsertFromClaims(context.Background(), claims, "loom-admins")
	if err != nil {
		t.Fatalf("second upsert error: %v", err)
	}
	if user.Role != RoleUser {
		t.Fatalf("role after refresh = %q, want user", user.Role)
	}
	if user.Username != "jan" {
		t.Fatalf("username = %q, want jan", user.Username)
	}
}

func TestUserStore_UpsertFromClaimsFallsBackToEmailForUsername(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)

	user, err := store.UpsertFromClaims(context.Background(), Claims{
		Subject: "authentik-sub-2",
		Email:   "user@example.com",
	}, "loom-admins")
	if err != nil {
		t.Fatalf("UpsertFromClaims() error: %v", err)
	}
	if user.Username != "user@example.com" {
		t.Fatalf("username = %q, want email fallback", user.Username)
	}
}

func TestUserStore_UpsertFromClaimsAdoptsAccountWhenSubjectChanges(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)
	ctx := context.Background()

	before, err := store.UpsertFromClaims(ctx, Claims{
		Subject:  "authentik-sub",
		Username: "jan",
		Email:    "jan@example.com",
		Name:     "Jan",
	}, "loom-admins")
	if err != nil {
		t.Fatalf("first upsert error: %v", err)
	}
	if err := store.UpdateResponseLanguage(ctx, before.ID, "en"); err != nil {
		t.Fatalf("UpdateResponseLanguage() error: %v", err)
	}

	// Same person, same email, brand-new subject from a different provider.
	after, err := store.UpsertFromClaims(ctx, Claims{
		Subject:  "authelia-sub",
		Username: "jan",
		Email:    "JAN@example.com",
		Name:     "Jan Saner",
		Groups:   []string{"loom-admins"},
	}, "loom-admins")
	if err != nil {
		t.Fatalf("second upsert error: %v", err)
	}

	if after.ID != before.ID {
		t.Fatalf("id = %q, want the existing %q (threads hang off this id)", after.ID, before.ID)
	}
	if after.OIDCSubject != "authelia-sub" {
		t.Fatalf("oidc subject = %q, want authelia-sub", after.OIDCSubject)
	}
	if after.ResponseLanguage != "en" {
		t.Fatalf("response language = %q, want en", after.ResponseLanguage)
	}
	if after.DisplayName != "Jan Saner" {
		t.Fatalf("display name = %q, want the refreshed name", after.DisplayName)
	}
	if after.Role != RoleAdmin {
		t.Fatalf("role = %q, want admin", after.Role)
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
}

func TestUserStore_UpsertFromClaimsDoesNotAdoptAmbiguousEmail(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)
	ctx := context.Background()

	// Two rows already share an email (as a half-migrated database does), so
	// there is no single account to adopt.
	for _, subject := range []string{"old-sub", "new-sub"} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, email, display_name, role, response_language, last_seen_at)
VALUES (?, ?, 'jan', 'jan@example.com', 'Jan', 'user', '', datetime('now'))`,
			newID(), subject,
		); err != nil {
			t.Fatalf("seed %s: %v", subject, err)
		}
	}

	user, err := store.UpsertFromClaims(ctx, Claims{
		Subject: "third-sub",
		Email:   "jan@example.com",
	}, "loom-admins")
	if err != nil {
		t.Fatalf("UpsertFromClaims() error: %v", err)
	}
	if user.OIDCSubject != "third-sub" {
		t.Fatalf("oidc subject = %q, want a fresh third-sub row", user.OIDCSubject)
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(users))
	}
}

func TestUserStore_UpsertFromClaimsDoesNotAdoptWithoutEmail(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)
	ctx := context.Background()

	if _, err := store.UpsertFromClaims(ctx, Claims{
		Subject:  "old-sub",
		Username: "svc",
	}, "loom-admins"); err != nil {
		t.Fatalf("first upsert error: %v", err)
	}

	if _, err := store.UpsertFromClaims(ctx, Claims{
		Subject:  "new-sub",
		Username: "svc",
	}, "loom-admins"); err != nil {
		t.Fatalf("second upsert error: %v", err)
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2 (no email, no adoption)", len(users))
	}
}

func TestUserStore_ListUsersOrdersByUsername(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)

	for _, claims := range []Claims{
		{Subject: "sub-b", Username: "zoe"},
		{Subject: "sub-a", Username: "amy"},
	} {
		if _, err := store.UpsertFromClaims(context.Background(), claims, "loom-admins"); err != nil {
			t.Fatalf("upsert %s: %v", claims.Subject, err)
		}
	}

	users, err := store.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].Username != "amy" || users[1].Username != "zoe" {
		t.Fatalf("user order = %q, %q; want amy, zoe", users[0].Username, users[1].Username)
	}
}
