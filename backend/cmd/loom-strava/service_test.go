package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	good := map[string]string{"STRAVA_TOKEN": testToken, "STRAVA_CLIENT_ID": "1", "STRAVA_CLIENT_SECRET": "s"}
	cfg, err := loadConfig(env(good))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != ":8050" || cfg.data != "/data" || cfg.base.String() != "https://www.strava.com" {
		t.Errorf("defaults: %+v", cfg)
	}
	for name, m := range map[string]map[string]string{
		"short token":  {"STRAVA_TOKEN": "short", "STRAVA_CLIENT_ID": "1", "STRAVA_CLIENT_SECRET": "s"},
		"space token":  {"STRAVA_TOKEN": "test-service-token 0123456789abcdef", "STRAVA_CLIENT_ID": "1", "STRAVA_CLIENT_SECRET": "s"},
		"no client":    {"STRAVA_TOKEN": testToken, "STRAVA_CLIENT_SECRET": "s"},
		"no secret":    {"STRAVA_TOKEN": testToken, "STRAVA_CLIENT_ID": "1"},
		"bad base url": {"STRAVA_TOKEN": testToken, "STRAVA_CLIENT_ID": "1", "STRAVA_CLIENT_SECRET": "s", "STRAVA_BASE_URL": "ftp://x"},
	} {
		if _, err := loadConfig(env(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStore_stateIsPrivateAndPerUser(t *testing.T) {
	dir := t.TempDir()
	s := fileStore{dir: dir}
	st, err := s.load()
	if err != nil || len(st.Users) != 0 {
		t.Fatalf("empty load: %v %v", st, err)
	}
	st.Users[userA] = connection{AthleteID: 1, AccessToken: "a"}
	st.Users[userB] = connection{AthleteID: 2, AccessToken: "b"}
	st.Users["../evil"] = connection{AthleteID: 3}
	if err := s.save(st); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, stateFile))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode: %v %v", fi, err)
	}
	got, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Users[userA].AccessToken != "a" || got.Users[userB].AccessToken != "b" {
		t.Errorf("round trip: %+v", got)
	}
	if _, ok := got.Users["../evil"]; ok {
		t.Error("an invalid user id survived a load")
	}
	// The file names the fields loom's operators read.
	b, _ := os.ReadFile(filepath.Join(dir, stateFile))
	for _, k := range []string{`"users"`, `"athlete_id"`, `"access_token"`, `"refresh_token"`, `"expires_at"`, `"connected_at"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("state file lacks %s: %s", k, b)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.load(); err == nil {
		t.Error("a broken state file loaded")
	}

	if _, err := s.metricsPath("../x"); err == nil {
		t.Error("metrics path for an invalid id")
	}
	m := metricFile{Activities: map[string]storedMetrics{"1": {V: metricsVersion, Start: "2026-01-01", MovingS: 10, Vals: map[string]float64{"np": 200}}}}
	if err := s.saveMetrics(userA, m); err != nil {
		t.Fatal(err)
	}
	fi, err = os.Stat(filepath.Join(dir, metricsDir, userA+".json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("metric file mode: %v %v", fi, err)
	}
	if got, _ := s.loadMetrics(userB); len(got.Activities) != 0 {
		t.Error("user B read user A's metrics")
	}
	if got, _ := s.loadMetrics(userA); got.Activities["1"].Vals["np"] != 200 {
		t.Errorf("metrics round trip: %+v", got)
	}
	if err := s.deleteMetrics(userA); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteMetrics(userA); err != nil {
		t.Error("deleting a missing metric file must succeed")
	}
	acts := map[string]storedMetrics{"a": {Start: "2026-01-03"}, "b": {Start: "2026-01-01"}, "c": {Start: "2026-01-02"}}
	pruneMetrics(acts, 2)
	if _, ok := acts["b"]; ok || len(acts) != 2 {
		t.Errorf("prune kept the oldest: %v", acts)
	}
}

func TestRateBook(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 7, 0, 0, time.UTC)
	b := newRateBook()
	if _, blocked := b.blocked(now); blocked {
		t.Fatal("blocked before any call")
	}
	h := http.Header{}
	h.Set("X-ReadRateLimit-Limit", "100,1000")
	h.Set("X-ReadRateLimit-Usage", "89,300")
	b.observe(h, now)
	if _, blocked := b.blocked(now); blocked {
		t.Error("89 of 100 is not yet 90 %")
	}
	h.Set("X-ReadRateLimit-Usage", "90,300")
	b.observe(h, now)
	reset, blocked := b.blocked(now)
	if !blocked || reset.Format("15:04") != "10:15" {
		t.Errorf("at 90 %%: blocked %v until %v", blocked, reset)
	}
	if _, blocked := b.blocked(now.Add(8 * time.Minute)); blocked {
		t.Error("a new 15-minute window starts at zero")
	}
	h.Set("X-ReadRateLimit-Usage", "3,950")
	b.observe(h, now)
	if reset, blocked := b.blocked(now); !blocked || reset.Format("2006-01-02 15:04") != "2026-11-04 00:00" {
		t.Errorf("daily: %v %v", blocked, reset)
	}
	if msg := (&rateLimitedError{reset: reset}).Error(); msg != "Strava rate limit reached: resets at 10:15 (UTC)" {
		t.Errorf("message: %q", msg)
	}

	// Without headers the book counts by hand from Strava's defaults.
	b = newRateBook()
	for range 50 {
		b.observe(http.Header{}, now)
	}
	if f := b.fraction15(now); f != 0.5 {
		t.Errorf("fraction after 50 unheaded calls = %v", f)
	}
	b.exhaust(http.Header{}, now)
	if _, blocked := b.blocked(now); !blocked {
		t.Error("a 429 must block")
	}
	for _, v := range []string{"", "1", "a,b", "-1,2"} {
		if _, _, ok := pair(v); ok {
			t.Errorf("pair(%q) parsed", v)
		}
	}
}

func TestRoutes_auth(t *testing.T) {
	f := newFakeStrava(t)
	_, h, _ := newTestService(t, f)
	// Every route behind the token (the token rules: sidecar.TestHandler).
	for _, r := range [][2]string{{"GET", "/status"}, {"GET", "/authorize"}, {"PUT", "/connection"}, {"DELETE", "/connection"}, {"POST", "/mcp"}} {
		req := httptest.NewRequest(r[0], r[1], nil)
		req.Header.Set("X-Loom-User", userA)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%v without bearer: %d", r, rec.Code)
		}
	}
	for _, r := range [][2]string{{"GET", "/status"}, {"GET", "/authorize"}, {"PUT", "/connection"}, {"DELETE", "/connection"}} {
		if code, body := request(t, h, r[0], r[1], "", ""); code != http.StatusBadRequest || body["code"] != codeMalformed {
			t.Errorf("%v without X-Loom-User: %d %v", r, code, body)
		}
	}
	for _, bad := range []string{"../x", "a/b", strings.Repeat("x", 65), "a b"} {
		for _, p := range []string{"/status", "/mcp"} {
			if code, _ := request(t, h, "GET", p, bad, ""); code != http.StatusBadRequest {
				t.Errorf("%s with user %q: %d", p, bad, code)
			}
		}
	}
	// Discovery carries no user; a tool call without one is an error result.
	code, _ := request(t, h, "POST", "/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK {
		t.Errorf("tools/list without a user: %d", code)
	}
	if r := callTool(t, h, "", "profile", `{}`); !r.IsError {
		t.Errorf("tool call without a user: %+v", r)
	}
}

func TestToolList(t *testing.T) {
	f := newFakeStrava(t)
	_, h, _ := newTestService(t, f)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var r struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range r.Result.Tools {
		names = append(names, x.Name)
	}
	want := "profile hr_zones run_bests efficiency_trend totals list_activities get_activity activity_analysis compare_activities gear routes segments"
	if strings.Join(names, " ") != want {
		t.Errorf("tools = %v", names)
	}
}

// The instructions travel with the tools into every turn that offers them
// (training, health, or a drifted turn in any thread), so a symptom the user
// ties to their sport is read against their activities there, whatever the
// thread's category block says.
func TestInstructions_symptomTiedToSport(t *testing.T) {
	for _, want := range []string{"symptom", "list_activities", "possible contributors", "doctor"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	// loom caps one server's instructions at 3 KiB (maxServerInstructionsBytes):
	// past it the tail, this rule included, is cut.
	if len(instructions) > 3<<10 {
		t.Errorf("instructions are %d bytes, loom keeps 3072", len(instructions))
	}
}

func TestConnection_lifecycle(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	_, h, dir := newTestService(t, f)

	if code, st := request(t, h, "GET", "/status", userA, ""); code != 200 || st["state"] != "never" {
		t.Fatalf("status before: %d %v", code, st)
	}
	code, body := request(t, h, "GET", "/authorize?redirect_uri="+url.QueryEscape("https://loom.example/api/strava/callback")+"&state=nonce1", userA, "")
	if code != 200 {
		t.Fatalf("authorize: %d %v", code, body)
	}
	u, err := url.Parse(body["url"].(string))
	if err != nil || u.Path != "/oauth/authorize" || !strings.HasPrefix(body["url"].(string), f.srv.URL) {
		t.Fatalf("authorize url: %v", body["url"])
	}
	q := u.Query()
	if q.Get("client_id") != testClientID || q.Get("scope") != "read_all,profile:read_all,activity:read_all" ||
		q.Get("approval_prompt") != "force" || q.Get("response_type") != "code" || q.Get("state") != "nonce1" ||
		q.Get("redirect_uri") != "https://loom.example/api/strava/callback" {
		t.Errorf("authorize query: %v", q)
	}
	for _, bad := range []string{"/authorize?redirect_uri=javascript:alert(1)&state=x", "/authorize?redirect_uri=/relative&state=x",
		"/authorize?redirect_uri=" + url.QueryEscape("https://x.example/cb")} {
		if code, body := request(t, h, "GET", bad, userA, ""); code != 422 || body["code"] != codeMalformed {
			t.Errorf("%s: %d %v", bad, code, body)
		}
	}

	if code, body := request(t, h, "PUT", "/connection", userA, `{"code":""}`); code != 422 || body["code"] != codeMalformed {
		t.Errorf("empty code: %d %v", code, body)
	}
	if code, _ := request(t, h, "PUT", "/connection", userA, `{`); code != 400 {
		t.Errorf("bad json: %d", code)
	}
	if code, body := request(t, h, "PUT", "/connection", userA, `{"code":"wrong"}`); code != 422 || body["code"] != codeRejected {
		t.Errorf("wrong code: %d %v", code, body)
	}
	st := connect(t, h, userA, "code-a")
	if st["state"] != "connected" || st["athlete"].(map[string]any)["name"] != "Anna Test" {
		t.Errorf("connected: %v", st)
	}
	if m := st["missingScopes"].([]any); len(m) != 0 {
		t.Errorf("missing scopes: %v", m)
	}
	b, _ := os.ReadFile(filepath.Join(dir, stateFile))
	if !strings.Contains(string(b), `"user-a"`) || !strings.Contains(string(b), "acc-1-") {
		t.Errorf("state file: %s", b)
	}

	// /status?check=1 makes one call.
	before := f.count("/athlete")
	if code, st := request(t, h, "GET", "/status?check=1", userA, ""); code != 200 || st["state"] != "connected" || st["lastOkAt"] == nil {
		t.Errorf("check: %d %v", code, st)
	}
	if f.count("/athlete") != before+1 {
		t.Errorf("check made %d calls", f.count("/athlete")-before)
	}

	// Disconnect revokes the refresh token and deletes everything kept.
	if err := (fileStore{dir: dir}).saveMetrics(userA, metricFile{Activities: map[string]storedMetrics{"1": {}}}); err != nil {
		t.Fatal(err)
	}
	if code, st := request(t, h, "DELETE", "/connection", userA, ""); code != 200 || st["state"] != "never" {
		t.Errorf("disconnect: %d %v", code, st)
	}
	if len(f.revoked) != 1 || !strings.HasPrefix(f.revoked[0], "ref-1-") || len(f.deauthorized) != 0 {
		t.Errorf("revoked %v, deauthorized %v", f.revoked, f.deauthorized)
	}
	if _, err := os.Stat(filepath.Join(dir, metricsDir, userA+".json")); !os.IsNotExist(err) {
		t.Error("metric file kept after disconnect")
	}
	b, _ = os.ReadFile(filepath.Join(dir, stateFile))
	if strings.Contains(string(b), "user-a") {
		t.Errorf("entry kept after disconnect: %s", b)
	}
	if r := callTool(t, h, userA, "profile", `{}`); !r.IsError || r.Text != msgNotConnected {
		t.Errorf("after disconnect: %+v", r)
	}
}

// Unticked boxes count even when the token answer lists every scope: the
// consent page's report is what the user granted.
func TestConnection_missingScopesFromConsentPage(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	_, h, _ := newTestService(t, f)
	st := connectScope(t, h, userA, "code-a", "read,activity:read_all")
	got, _ := json.Marshal(st["scopes"])
	if string(got) != `["read","activity:read_all"]` {
		t.Errorf("scopes = %s", got)
	}
	missing, _ := json.Marshal(st["missingScopes"])
	if string(missing) != `["read_all","profile:read_all"]` {
		t.Errorf("missingScopes = %s", missing)
	}
}

// Without the consent page's report the token answer's spaced list counts,
// stored comma-joined.
func TestConnection_tokenAnswerScopeFallback(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	_, h, dir := newTestService(t, f)
	st := connectScope(t, h, userA, "code-a", "")
	if m := st["missingScopes"].([]any); len(m) != 0 {
		t.Errorf("missingScopes = %v", m)
	}
	b, _ := os.ReadFile(filepath.Join(dir, stateFile))
	if !strings.Contains(string(b), `"scope":"activity:read_all,profile:read_all,read,read_all"`) {
		t.Errorf("state file: %s", b)
	}
}

// A connection stored while the token answer's spaces went unsplit reads
// whole without reconnecting.
func TestConnection_scopeSeparators(t *testing.T) {
	stored := "activity:read_all profile:read_all read read_all"
	if got := missingScopes(splitScopes(stored)); len(got) != 0 {
		t.Errorf("stored %q: missing %v", stored, got)
	}
	got := splitScopes(" read,activity:read_all  read\tread_all,")
	if !slices.Equal(got, []string{"read", "activity:read_all", "read_all"}) {
		t.Errorf("mixed separators: %v", got)
	}
}

func TestConnection_disconnectSucceedsWhenStravaFails(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	f.revokeOK = false
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	if code, st := request(t, h, "DELETE", "/connection", userA, ""); code != 200 || st["state"] != "never" {
		t.Errorf("disconnect: %d %v", code, st)
	}
	// Not connected: nothing to revoke, still fine.
	if code, _ := request(t, h, "DELETE", "/connection", userB, ""); code != 200 {
		t.Errorf("disconnect of nobody: %d", code)
	}
}

// Where Strava has no /oauth/revoke (404), the legacy /oauth/deauthorize
// ends the authorization with the access token.
func TestConnection_disconnectFallsBackToDeauthorize(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	f.revokeMissing = true
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	c, _ := svc.conn(userA)
	if code, st := request(t, h, "DELETE", "/connection", userA, ""); code != 200 || st["state"] != "never" {
		t.Errorf("disconnect: %d %v", code, st)
	}
	if len(f.revoked) != 0 || len(f.deauthorized) != 1 || f.deauthorized[0] != c.AccessToken {
		t.Errorf("revoked %v, deauthorized %v, want the access token %s", f.revoked, f.deauthorized, c.AccessToken)
	}
	if _, ok := svc.conn(userA); ok {
		t.Error("entry kept")
	}
}

// One user's slow state write holds up nobody else: lookups and cached
// reads never wait for the disk.
func TestSave_slowWriteBlocksNoOtherUser(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	f.addAthlete(2, "Ben", "code-b", 300)
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	connect(t, h, userB, "code-b")
	if r := callTool(t, h, userB, "profile", `{}`); r.IsError {
		t.Fatal(r.Text) // warms B's cache
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc.beforeSave = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	ca, _ := svc.conn(userA)
	saved := make(chan struct{})
	go func() {
		svc.record(userA, ca.AccessToken, errPermission) // writes state.json
		close(saved)
	}()
	<-entered
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := svc.conn(userB); !ok {
			t.Error("B not connected")
		}
		if a, err := svc.athlete(context.Background(), userB); err != nil || a.Firstname != "Ben" {
			t.Errorf("B's cached athlete: %+v %v", a, err)
		}
		_ = svc.status(userA) // A's own lookups do not wait either
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("another user's lookup waited for A's write")
	}
	close(release)
	<-saved
	<-done
	if st := svc.status(userA); st.State != "error" {
		t.Errorf("A's state after the write: %+v", st)
	}
}

// Snapshots land in order: an older one never overwrites a newer one.
func TestSave_inOrder(t *testing.T) {
	dir := t.TempDir()
	svc := &service{store: fileStore{dir: dir}, st: state{Users: map[string]connection{}}, slots: map[string]*slot{}}
	svc.mu.Lock()
	svc.st.Users[userA] = connection{AccessToken: "old"}
	old, oldGen := svc.snapshotLocked()
	svc.st.Users[userA] = connection{AccessToken: "new"}
	cur, curGen := svc.snapshotLocked()
	svc.mu.Unlock()
	if err := svc.persist(cur, curGen); err != nil {
		t.Fatal(err)
	}
	if err := svc.persist(old, oldGen); err != nil {
		t.Fatal(err)
	}
	st, err := svc.store.load()
	if err != nil || st.Users[userA].AccessToken != "new" {
		t.Errorf("on disk: %+v %v", st.Users[userA], err)
	}
}

func TestConnection_upstreamDown(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	_, h, _ := newTestService(t, f)
	f.srv.Close()
	if code, body := request(t, h, "PUT", "/connection", userA, `{"code":"code-a"}`); code != 502 || body["code"] != codeUpstream {
		t.Errorf("down: %d %v", code, body)
	}
}

func TestConnection_storageFailure(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	svc, h, dir := newTestService(t, f)
	svc.store = fileStore{dir: filepath.Join(dir, "missing")}
	if code, body := request(t, h, "PUT", "/connection", userA, `{"code":"code-a"}`); code != 500 || body["code"] != codeStorage {
		t.Errorf("storage: %d %v", code, body)
	}
	if svc.status(userA).State != "never" {
		t.Error("a connection that could not be stored is in memory")
	}
}

// The refreshed pair is on disk before any call uses it: Strava kills the
// old refresh token at once, so a crash in between must not lose it.
func TestRefresh_rotatesAndPersistsBeforeUse(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	old, _ := svc.conn(userA)
	f.onAPI = func(r *http.Request, bearer string) {
		b, _ := os.ReadFile(filepath.Join(dir, stateFile))
		if !strings.Contains(string(b), bearer) {
			t.Errorf("%s called with a token not yet on disk", r.URL.Path)
		}
	}
	f.advance(6*time.Hour - 4*time.Minute) // inside the 5-minute margin
	if r := callTool(t, h, userA, "profile", `{}`); r.IsError {
		t.Fatalf("profile: %s", r.Text)
	}
	if f.count("token:refresh_token") != 1 {
		t.Errorf("refreshes = %d", f.count("token:refresh_token"))
	}
	cur, _ := svc.conn(userA)
	if cur.RefreshToken == old.RefreshToken || cur.AccessToken == old.AccessToken {
		t.Error("pair not rotated")
	}
	// The old refresh token is dead at Strava now.
	_, code, err := svc.postToken(context.Background(), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old.RefreshToken}})
	if code != 400 || err == nil {
		t.Errorf("old refresh token: %d %v", code, err)
	}
}

func TestRefresh_concurrentCallsRefreshOnce(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	f.slowToken = 50 * time.Millisecond
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	f.advance(7 * time.Hour)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := svc.accessToken(context.Background(), userA)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := f.count("token:refresh_token"); n != 1 {
		t.Errorf("refreshes = %d, want 1", n)
	}
}

func TestRefresh_on401(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	// Strava revoked the access token early; the refresh token still works.
	c, _ := svc.conn(userA)
	f.mu.Lock()
	delete(f.access, c.AccessToken)
	f.mu.Unlock()
	if r := callTool(t, h, userA, "profile", `{}`); r.IsError {
		t.Fatalf("profile after a 401: %s", r.Text)
	}
	if f.count("token:refresh_token") != 1 || f.count("/athlete") != 2 {
		t.Errorf("refreshes %d, athlete calls %d", f.count("token:refresh_token"), f.count("/athlete"))
	}
}

func TestRefresh_invalidGrantMarksExpired(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	c, _ := svc.conn(userA)
	f.mu.Lock()
	f.dead[c.RefreshToken] = true // the user revoked the app at strava.com
	f.mu.Unlock()
	f.advance(7 * time.Hour)
	if r := callTool(t, h, userA, "profile", `{}`); !r.IsError || r.Text != msgExpired {
		t.Errorf("expired: %+v", r)
	}
	if code, st := request(t, h, "GET", "/status", userA, ""); code != 200 || st["state"] != "expired" {
		t.Errorf("status: %v", st)
	}
	// A later call does not even try.
	if r := callTool(t, h, userA, "totals", `{}`); r.Text != msgExpired || f.count("token:refresh_token") != 1 {
		t.Errorf("second call: %q, %d refreshes", r.Text, f.count("token:refresh_token"))
	}
	// Reconnecting heals it.
	f.addAthlete(1, "Anna", "code-a2", 265)
	if st := connect(t, h, userA, "code-a2"); st["state"] != "connected" {
		t.Errorf("reconnect: %v", st)
	}
}

func TestPermissionMissing(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	// Every token, fresh ones too, is refused: a scope is missing.
	f.onAPI = func(*http.Request, string) {
		f.mu.Lock()
		f.access = map[string]tokenInfo{}
		f.mu.Unlock()
	}
	if r := callTool(t, h, userA, "profile", `{}`); !r.IsError || r.Text != msgPermission {
		t.Errorf("permission: %+v", r)
	}
	if st := svc.status(userA); st.State != "error" || st.LastError == "" {
		t.Errorf("status: %+v", st)
	}
}

func TestRateLimit_refusesWithoutCalling(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	f.rateUsed15 = 89 // the next answer reports 90 of 100
	if r := callTool(t, h, userA, "profile", `{}`); r.IsError {
		t.Fatalf("profile: %s", r.Text)
	}
	calls := f.count("/athlete/stats")
	r := callTool(t, h, userA, "totals", `{}`)
	if !r.IsError || r.Text != "Strava rate limit reached: resets at 10:15 (UTC)" {
		t.Errorf("rate: %+v", r)
	}
	if f.count("/athletes/1/stats") != calls {
		t.Error("called Strava above the budget")
	}
	f.advance(15 * time.Minute)
	f.rateUsed15 = 0
	if r := callTool(t, h, userA, "totals", `{}`); r.IsError {
		t.Errorf("after the reset: %s", r.Text)
	}
}

func TestRateLimit_429(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	f.tooMany = true
	if r := callTool(t, h, userA, "profile", `{}`); !r.IsError || !strings.HasPrefix(r.Text, "Strava rate limit reached: resets at 10:15") {
		t.Errorf("429: %+v", r)
	}
}

// Two loom users, two Strava athletes: each call reads, uses and deletes
// only the caller's own token, cache and metrics.
func TestIsolation_twoUsers(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	f.addAthlete(2, "Ben", "code-b", 300)
	ride := synthRide(false)
	f.addActivity(1, 1001, "Anna's ride", "Ride", time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), &ride, nil)
	f.addActivity(2, 2001, "Ben's ride", "Ride", time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), &ride, nil)
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	connect(t, h, userB, "code-b")

	ca, _ := svc.conn(userA)
	cb, _ := svc.conn(userB)
	if ca.AccessToken == cb.AccessToken || ca.AthleteID != 1 || cb.AthleteID != 2 {
		t.Fatalf("connections: %+v %+v", ca, cb)
	}
	if r := callTool(t, h, userA, "profile", `{}`); !strings.Contains(r.Text, "Anna") || strings.Contains(r.Text, "Ben") {
		t.Errorf("A's profile: %s", r.Text)
	}
	if r := callTool(t, h, userB, "profile", `{}`); !strings.Contains(r.Text, "Ben") || strings.Contains(r.Text, "Anna") {
		t.Errorf("B's profile: %s", r.Text)
	}
	// A's cached profile is not B's: B's call above went to Strava.
	if f.count("/athlete") != 2 {
		t.Errorf("athlete calls = %d, want one per user", f.count("/athlete"))
	}
	// A cannot read B's activity, by id or by cache.
	if r := callTool(t, h, userB, "get_activity", `{"id":2001}`); r.IsError {
		t.Fatalf("B's own: %s", r.Text)
	}
	if r := callTool(t, h, userA, "get_activity", `{"id":2001}`); !r.IsError || !strings.Contains(r.Text, "not one of the user's activities") {
		t.Errorf("A reading B's activity: %+v", r)
	}
	for _, s := range f.seen {
		if strings.HasPrefix(s, "1:") && strings.Contains(s, "2001") {
			continue // asked with A's token, answered 404 by Strava
		}
		if strings.HasPrefix(s, "2:") && strings.Contains(s, "1001") {
			t.Errorf("B's token read A's activity: %s", s)
		}
	}
	// Metrics stay per user.
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatalf("A's analysis: %s", r.Text)
	}
	if r := callTool(t, h, userB, "activity_analysis", `{"id":2001}`); r.IsError {
		t.Fatalf("B's analysis: %s", r.Text)
	}
	ma, _ := svc.store.loadMetrics(userA)
	mb, _ := svc.store.loadMetrics(userB)
	if _, ok := ma.Activities["2001"]; ok {
		t.Error("B's activity in A's metrics")
	}
	if _, ok := mb.Activities["1001"]; ok {
		t.Error("A's activity in B's metrics")
	}
	if _, ok := mb.Activities["2001"]; !ok {
		t.Error("B's metrics missing")
	}
	// A disconnects: B keeps token, cache and metrics.
	cacheB := svc.cache(userB).profile.len()
	if code, _ := request(t, h, "DELETE", "/connection", userA, ""); code != 200 {
		t.Fatal(code)
	}
	if len(f.revoked) != 1 || f.revoked[0] != ca.RefreshToken {
		t.Errorf("revoked %v, want only A's", f.revoked)
	}
	if _, ok := svc.conn(userB); !ok {
		t.Error("B's connection gone")
	}
	if _, err := os.Stat(filepath.Join(dir, metricsDir, userB+".json")); err != nil {
		t.Error("B's metric file gone")
	}
	if svc.cache(userB).profile.len() != cacheB {
		t.Error("B's cache dropped")
	}
	if svc.cache(userA).profile.len() != 0 {
		t.Error("A's cache kept")
	}
	if r := callTool(t, h, userB, "profile", `{}`); r.IsError {
		t.Errorf("B after A left: %s", r.Text)
	}
}

func TestLRU(t *testing.T) {
	now := time.Now()
	c := newLRU(2, time.Minute)
	c.put("a", 1, now)
	c.put("b", 2, now)
	c.get("a", now)
	c.put("c", 3, now) // b is the least recently used
	if _, ok := c.get("b", now); ok {
		t.Error("b survived")
	}
	if v, ok := c.get("a", now); !ok || v != 1 {
		t.Error("a evicted")
	}
	c.put("a", 9, now)
	if v, _ := c.get("a", now); v != 9 {
		t.Error("update lost")
	}
	if _, ok := c.get("a", now.Add(time.Minute)); ok {
		t.Error("expired entry served")
	}
	if c.len() != 1 {
		t.Errorf("len = %d", c.len())
	}
}
