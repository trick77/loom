package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/loom/internal/mcpserver"
)

func ctxFor(user string) context.Context { return mcpserver.WithUser(context.Background(), user) }

// Two loom users, two Apple Music accounts: each call reads its caller's
// token only, and one user's changes never touch the other's account.
func TestService_accountsArePerUser(t *testing.T) {
	svc, api, _ := newTestService(t)
	if _, err := svc.SetToken(context.Background(), "alice", "good-alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Playlists(ctxFor("bob")); err != errNotConnected {
		t.Fatalf("bob reads with alice's token: %v", err)
	}
	if st := svc.status("bob"); st.State != "never" {
		t.Fatalf("bob sees alice's status: %+v", st)
	}
	if _, err := svc.SetToken(context.Background(), "bob", "good-bob"); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"alice", "bob"} {
		if _, _, err := svc.Playlists(ctxFor(user)); err != nil || api.lastTok != "good-"+user {
			t.Fatalf("%s: token %q, err %v", user, api.lastTok, err)
		}
	}
	// A failure on alice's token marks alice's account only.
	svc.record("alice", "good-alice", applemusic.ErrUserTokenExpired, nil)
	if svc.status("alice").State != "expired" || svc.status("bob").State != "connected" {
		t.Fatalf("alice %+v, bob %+v", svc.status("alice"), svc.status("bob"))
	}
	if _, err := svc.ClearToken("alice"); err != nil {
		t.Fatal(err)
	}
	if svc.status("alice").State != "never" || svc.status("bob").State != "connected" {
		t.Fatal("clearing alice touched bob")
	}
	if _, _, err := svc.Playlists(context.Background()); err != errNoUser {
		t.Fatalf("a call for nobody: %v", err)
	}
}

// The library artist cache keeps one entry per user.
func TestLibraryArtists_cachePerUser(t *testing.T) {
	svc, api, _ := newTestService(t)
	svc.now = func() time.Time { return now }
	for _, u := range []string{"alice", "bob"} {
		if _, err := svc.SetToken(context.Background(), u, "good-"+u); err != nil {
			t.Fatal(err)
		}
	}
	api.libArtists = []applemusic.Artist{{ID: "l.1", Name: "A"}}
	for _, u := range []string{"alice", "bob", "alice"} {
		if _, _, err := svc.LibraryArtists(ctxFor(u)); err != nil {
			t.Fatal(err)
		}
	}
	if api.calls["artists"] != 2 {
		t.Fatalf("artist reads = %d, want one per user", api.calls["artists"])
	}
}

// The token routes refuse a request that names no user. Tool discovery needs
// none (loom lists the tools for nobody in particular); a library tool call
// without one reads no account.
func TestServer_perUserRoutesNeedTheUser(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	h := newServer(config{token: testToken}, svc, "test")
	send := func(method, path, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, r := range [][3]string{{"GET", "/status", ""}, {"PUT", "/token", `{"token":"good"}`}, {"DELETE", "/token", ""}} {
		if code, _ := send(r[0], r[1], r[2]); code != http.StatusBadRequest {
			t.Errorf("%s %s without a user: %d", r[0], r[1], code)
		}
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, path := range []string{"/mcp/library", "/mcp/catalog"} {
		if code, body := send("POST", path, list); code != http.StatusOK || !strings.Contains(body, `"tools"`) {
			t.Errorf("discovery on %s without a user: %d %s", path, code, body)
		}
	}
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_playlists","arguments":{}}}`
	if _, body := send("POST", "/mcp/library", call); !strings.Contains(body, "need the signed-in loom user") || strings.Contains(body, "Playlist") {
		t.Errorf("a call for nobody: %s", body)
	}
	if svc.status(testUser).State != "connected" {
		t.Error("a call for nobody touched the account")
	}
}

// One user's slow token check never holds up another user's change.
func TestService_tokenChangesLockPerUser(t *testing.T) {
	svc, api, _ := newTestService(t)
	if _, err := svc.SetToken(context.Background(), "bob", "good-bob"); err != nil {
		t.Fatal(err)
	}
	inCheck, release := make(chan struct{}), make(chan struct{})
	api.sizeHook = func(context.Context) {
		close(inCheck)
		<-release
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.SetToken(context.Background(), "alice", "good-alice")
		done <- err
	}()
	<-inCheck
	cleared := make(chan error, 1)
	go func() {
		_, err := svc.ClearToken("bob")
		cleared <- err
	}()
	select {
	case err := <-cleared:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bob's disconnect waited on alice's check")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if svc.status("alice").State != "connected" || svc.status("bob").State != "never" {
		t.Fatalf("alice %+v bob %+v", svc.status("alice"), svc.status("bob"))
	}
}

// A legacy file on a /data that cannot be written still starts the sidecar,
// without the old token.
func TestStore_legacyTokenOnReadOnlyData(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"user_token":"someones"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	svc, err := newService(&fakeAPI{}, fileStore{dir: dir}, "ch")
	if err != nil {
		t.Fatalf("read-only /data kept the sidecar from starting: %v", err)
	}
	if svc.accounts() != 0 {
		t.Errorf("legacy token became an account")
	}
}

// A state file from before per-user accounts holds one shared token: it is
// dropped on load and the file rewritten, the developer token kept.
func TestStore_legacySharedTokenIsDropped(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"user_token":"someones","storefront":"ch","dev_token":"dev","dev_token_exp":"2030-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := newService(&fakeAPI{}, fileStore{dir: dir}, "ch")
	if err != nil {
		t.Fatal(err)
	}
	if svc.accounts() != 0 || svc.devToken().Value != "dev" {
		t.Fatalf("accounts %d, dev %q", svc.accounts(), svc.devToken().Value)
	}
	b, _ := os.ReadFile(filepath.Join(dir, stateFile))
	if strings.Contains(string(b), "someones") {
		t.Errorf("legacy token still on disk: %s", b)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil || raw["dev_token"] != "dev" {
		t.Errorf("rewritten file: %s (%v)", b, err)
	}
}
