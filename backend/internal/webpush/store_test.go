package webpush_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/trick77/loom/internal/store"
	"github.com/trick77/loom/internal/webpush"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "push_test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{"u1", "u2"} {
		if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`, id, "sub-"+id, id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	return db
}

func sub(n int) webpush.Subscription {
	return webpush.Subscription{Endpoint: fmt.Sprintf("https://web.push.apple.com/e%d", n), P256dh: "p", Auth: "a"}
}

func endpoints(t *testing.T, st *webpush.Store, userID string) []string {
	t.Helper()
	list, err := st.List(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, s := range list {
		out = append(out, s.Endpoint)
	}
	return out
}

func TestStoreIsScopedByUser(t *testing.T) {
	ctx := context.Background()
	st := webpush.NewStore(openDB(t))
	if err := st.Save(ctx, "u1", sub(1), "iPhone", "K"); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, "u2", sub(2), "Mac", "K"); err != nil {
		t.Fatal(err)
	}
	if got := endpoints(t, st, "u1"); len(got) != 1 || got[0] != sub(1).Endpoint {
		t.Fatalf("u1 sees %v", got)
	}
	if removed, err := st.Delete(ctx, "u2", sub(1).Endpoint); err != nil || removed {
		t.Fatalf("u2 removed u1's subscription: %v %v", removed, err)
	}
	if removed, err := st.Delete(ctx, "u1", sub(1).Endpoint); err != nil || !removed {
		t.Fatalf("u1 could not remove its own: %v %v", removed, err)
	}
	if got := endpoints(t, st, "u1"); len(got) != 0 {
		t.Fatalf("u1 still has %v", got)
	}
}

// The endpoint is one browser profile: the same browser signed in as someone
// else (same keys) moves it to them.
func TestSaveMovesAnEndpointWithItsOwnKeys(t *testing.T) {
	ctx := context.Background()
	st := webpush.NewStore(openDB(t))
	_ = st.Save(ctx, "u1", sub(1), "iPhone", "K")
	if err := st.Save(ctx, "u2", sub(1), "iPhone", "K2"); err != nil {
		t.Fatal(err)
	}
	if got := endpoints(t, st, "u1"); len(got) != 0 {
		t.Errorf("u1 keeps %v", got)
	}
	list, _ := st.List(ctx, "u2")
	if len(list) != 1 || list[0].VAPIDKey != "K2" {
		t.Errorf("u2 has %+v", list)
	}
}

// Knowing another user's endpoint is not owning it: other keys are refused.
func TestSaveRefusesAnotherUsersEndpointWithOtherKeys(t *testing.T) {
	ctx := context.Background()
	st := webpush.NewStore(openDB(t))
	_ = st.Save(ctx, "u1", sub(1), "iPhone", "K")
	stolen := sub(1)
	stolen.P256dh, stolen.Auth = "p2", "a2"
	if err := st.Save(ctx, "u2", stolen, "", "K"); !errors.Is(err, webpush.ErrTaken) {
		t.Fatalf("err %v, want ErrTaken", err)
	}
	list, _ := st.List(ctx, "u1")
	if len(list) != 1 || list[0].P256dh != "p" || list[0].Auth != "a" {
		t.Errorf("u1's row changed: %+v", list)
	}
	// Its own user renews the keys freely.
	if err := st.Save(ctx, "u1", stolen, "", "K"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveKeepsTheNewestPerUser(t *testing.T) {
	ctx := context.Background()
	st := webpush.NewStore(openDB(t))
	for i := range webpush.MaxSubscriptions + 2 {
		if err := st.Save(ctx, "u1", sub(i), "", "K"); err != nil {
			t.Fatal(err)
		}
	}
	got := endpoints(t, st, "u1")
	if len(got) != webpush.MaxSubscriptions {
		t.Fatalf("%d kept, want %d", len(got), webpush.MaxSubscriptions)
	}
	for _, e := range got {
		if e == sub(0).Endpoint || e == sub(1).Endpoint {
			t.Errorf("oldest %s kept", e)
		}
	}
}

func TestMarkSent(t *testing.T) {
	ctx := context.Background()
	st := webpush.NewStore(openDB(t))
	_ = st.Save(ctx, "u1", sub(1), "", "K")
	at := time.Date(2026, 11, 8, 7, 0, 0, 0, time.UTC)
	if err := st.MarkSent(ctx, "u2", sub(1).Endpoint, at); err != nil {
		t.Fatal(err)
	}
	list, _ := st.List(ctx, "u1")
	if list[0].LastSuccessAt != nil {
		t.Fatal("another user's send marked it")
	}
	_ = st.MarkSent(ctx, "u1", sub(1).Endpoint, at)
	list, _ = st.List(ctx, "u1")
	if list[0].LastSuccessAt == nil || !list[0].LastSuccessAt.Equal(at) {
		t.Errorf("last success %v", list[0].LastSuccessAt)
	}
}
