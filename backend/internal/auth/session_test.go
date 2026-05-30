package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func insertTestUser(t *testing.T, db DBTX, role Role) User {
	t.Helper()
	user := User{
		ID:               newID(),
		OIDCSubject:      newID(),
		Username:         "test-user",
		Email:            "test@example.com",
		DisplayName:      "Test User",
		Role:             role,
		ResponseLanguage: "auto",
	}
	_, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, email, display_name, role, response_language)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.OIDCSubject, user.Username, user.Email, user.DisplayName, user.Role, user.ResponseLanguage,
	)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return user
}

func TestSessionStore_CreateStoresOnlyHashAndFindsUser(t *testing.T) {
	db := openTestDB(t)
	user := insertTestUser(t, db, RoleUser)
	store := NewSessionStore(db, false)

	session, err := store.Create(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if session.Token == "" {
		t.Fatal("token is empty")
	}

	var rawCount int
	err = db.QueryRowContext(context.Background(), `SELECT count(*) FROM sessions WHERE token_hash = ?`, session.Token).Scan(&rawCount)
	if err != nil {
		t.Fatalf("raw token query: %v", err)
	}
	if rawCount != 0 {
		t.Fatal("raw token was stored")
	}

	got, ok, err := store.Lookup(context.Background(), session.Token)
	if err != nil || !ok {
		t.Fatalf("Lookup() = ok %v err %v", ok, err)
	}
	if got.UserID != user.ID {
		t.Fatalf("user id = %q, want %q", got.UserID, user.ID)
	}
}

func TestSessionStore_ExpiredSessionIsNotReturned(t *testing.T) {
	db := openTestDB(t)
	user := insertTestUser(t, db, RoleUser)
	store := NewSessionStore(db, false)

	session, err := store.Create(context.Background(), user.ID, -time.Minute)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	_, ok, err := store.Lookup(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if ok {
		t.Fatal("expired session returned ok")
	}
}

func TestSessionStore_RevokeRemovesSession(t *testing.T) {
	db := openTestDB(t)
	user := insertTestUser(t, db, RoleUser)
	store := NewSessionStore(db, false)

	session, err := store.Create(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := store.Revoke(context.Background(), session.Token); err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}

	_, ok, err := store.Lookup(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if ok {
		t.Fatal("revoked session returned ok")
	}
}

func TestSessionStore_CookieFlags(t *testing.T) {
	store := NewSessionStore(&sql.DB{}, true)
	cookie := store.CookieFor("abc", time.Now().Add(time.Hour))

	if cookie.Name != SessionCookieName {
		t.Fatalf("cookie name = %q", cookie.Name)
	}
	if !cookie.HttpOnly {
		t.Fatal("cookie is not HttpOnly")
	}
	if !cookie.Secure {
		t.Fatal("cookie is not Secure")
	}
	if cookie.Path != "/" {
		t.Fatalf("cookie path = %q, want /", cookie.Path)
	}
}
