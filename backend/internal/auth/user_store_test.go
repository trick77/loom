package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trick77/lume/internal/store"
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
		Groups:   []string{"slopr-admins"},
	}

	user, err := store.UpsertFromClaims(context.Background(), claims, "slopr-admins")
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
	user, err = store.UpsertFromClaims(context.Background(), claims, "slopr-admins")
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
	}, "slopr-admins")
	if err != nil {
		t.Fatalf("UpsertFromClaims() error: %v", err)
	}
	if user.Username != "user@example.com" {
		t.Fatalf("username = %q, want email fallback", user.Username)
	}
}

func TestUserStore_ListUsersOrdersByUsername(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)

	for _, claims := range []Claims{
		{Subject: "sub-b", Username: "zoe"},
		{Subject: "sub-a", Username: "amy"},
	} {
		if _, err := store.UpsertFromClaims(context.Background(), claims, "slopr-admins"); err != nil {
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
