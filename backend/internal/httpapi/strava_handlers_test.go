package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/strava"
)

// fakeStrava keeps one connection per loom user, like the sidecar.
type fakeStrava struct {
	conns       map[string]string // user → code it connected with
	checked     bool
	err         error
	connectErr  error
	redirectURI string
	state       string
}

func (f *fakeStrava) Status(_ context.Context, user string, check bool) (strava.Status, error) {
	f.checked = check
	if f.err != nil {
		return strava.Status{}, f.err
	}
	if f.conns[user] == "" {
		return strava.Status{State: "never"}, nil
	}
	return strava.Status{State: "connected", Athlete: &strava.Athlete{ID: 7, Name: "Rider " + user}}, nil
}

func (f *fakeStrava) AuthorizeURL(_ context.Context, _ string, redirectURI, state string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.redirectURI, f.state = redirectURI, state
	return "https://www.strava.com/oauth/authorize?state=" + url.QueryEscape(state), nil
}

func (f *fakeStrava) Connect(ctx context.Context, user, code, _ string) (strava.Status, error) {
	if f.connectErr != nil {
		return strava.Status{}, f.connectErr
	}
	if f.conns == nil {
		f.conns = map[string]string{}
	}
	f.conns[user] = code
	return f.Status(ctx, user, false)
}

func (f *fakeStrava) Disconnect(ctx context.Context, user string) (strava.Status, error) {
	delete(f.conns, user)
	return f.Status(ctx, user, false)
}

func stravaServer(user auth.User, sv StravaService, users ...string) http.Handler {
	return New(Deps{
		Auth:        auth.NewMiddleware(fakeSessionStore{userID: user.ID}, fakeUserStore{user: user, ok: true}),
		Sessions:    fakeSessionStore{userID: user.ID},
		Users:       fakeUserStore{user: user, ok: true},
		Strava:      sv,
		StravaUsers: users,
		PublicURL:   "https://loom.example",
	})
}

func stravaRequest(t *testing.T, h http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// nonce is the state Strava gets back: the cookie's value without its user.
func nonce(c *http.Cookie) string {
	n, _, _ := strings.Cut(c.Value, ".")
	return n
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestStrava_unlistedUsersGet404(t *testing.T) {
	sv := &fakeStrava{conns: map[string]string{"usr_a": "c"}}
	h := stravaServer(auth.User{ID: "usr_other"}, sv, "usr_a")
	for _, r := range [][2]string{
		{"GET", "/api/me/strava"}, {"POST", "/api/me/strava/check"}, {"GET", "/api/me/strava/connect"},
		{"DELETE", "/api/me/strava"}, {"GET", "/api/strava/callback?code=x&state=y"},
	} {
		if rec := stravaRequest(t, h, r[0], r[1]); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", r[0], r[1], rec.Code)
		}
	}
	if sv.conns["usr_a"] != "c" || len(sv.conns) != 1 {
		t.Errorf("an unlisted user changed the connections: %v", sv.conns)
	}
	// No sidecar: hidden for everyone.
	if rec := stravaRequest(t, stravaServer(auth.User{ID: "usr_a"}, nil, "usr_a"), "GET", "/api/me/strava"); rec.Code != http.StatusNotFound {
		t.Errorf("no sidecar: %d", rec.Code)
	}
}

func TestStrava_meFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		sv   StravaService
		list []string
		want bool
	}{
		"listed":     {&fakeStrava{}, []string{"usr_a"}, true},
		"unlisted":   {&fakeStrava{}, []string{"usr_b"}, false},
		"no sidecar": {nil, []string{"usr_a"}, false},
		"empty list": {&fakeStrava{}, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			rec := stravaRequest(t, stravaServer(auth.User{ID: "usr_a"}, tc.sv, tc.list...), "GET", "/api/me")
			var me struct {
				Strava bool `json:"strava"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.Strava != tc.want {
				t.Errorf("%s: %s, want strava %v", name, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestStrava_connectFlow(t *testing.T) {
	sv := &fakeStrava{}
	h := stravaServer(auth.User{ID: "usr_a"}, sv, "usr_a")

	rec := stravaRequest(t, h, "GET", "/api/me/strava/connect")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://www.strava.com/oauth/authorize") {
		t.Fatalf("connect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if sv.redirectURI != "https://loom.example/api/strava/callback" {
		t.Errorf("redirect_uri = %q", sv.redirectURI)
	}
	state := cookieNamed(rec, stravaStateCookie)
	if state == nil || state.Value != sv.state+".usr_a" || len(sv.state) < 32 || !state.HttpOnly || !state.Secure || state.Path != stravaCallbackPath || state.SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie: %+v (state %q)", state, sv.state)
	}

	// The answer must carry this browser's state.
	for _, target := range []string{
		"/api/strava/callback?code=c1&state=wrong",
		"/api/strava/callback?code=c1",
	} {
		rec = stravaRequest(t, h, "GET", target, state)
		if loc := rec.Header().Get("Location"); loc != "/?strava=state" {
			t.Errorf("%s: %s", target, loc)
		}
	}
	if rec = stravaRequest(t, h, "GET", "/api/strava/callback?code=c1&state="+nonce(state)); rec.Header().Get("Location") != "/?strava=state" {
		t.Errorf("no cookie: %s", rec.Header().Get("Location"))
	}
	if len(sv.conns) != 0 {
		t.Fatalf("connected without a matching state: %v", sv.conns)
	}
	// Another page sends the browser to a forged refusal: refused, and the
	// real answer still works afterwards.
	rec = stravaRequest(t, h, "GET", "/api/strava/callback?error=access_denied", state)
	if rec.Header().Get("Location") != "/?strava=state" || cookieNamed(rec, stravaStateCookie) != nil {
		t.Errorf("forged refusal: %s, cookie %+v", rec.Header().Get("Location"), cookieNamed(rec, stravaStateCookie))
	}
	rec = stravaRequest(t, h, "GET", "/api/strava/callback?error=access_denied&state="+nonce(state), state)
	if rec.Header().Get("Location") != "/?strava=denied" {
		t.Errorf("denied: %s", rec.Header().Get("Location"))
	}

	rec = stravaRequest(t, h, "GET", "/api/strava/callback?code=c1&scope=read_all&state="+nonce(state), state)
	if rec.Header().Get("Location") != "/?strava=connected" || sv.conns["usr_a"] != "c1" {
		t.Fatalf("callback: %s, conns %v", rec.Header().Get("Location"), sv.conns)
	}
	if c := cookieNamed(rec, stravaStateCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("state cookie not cleared: %+v", c)
	}

	sv.connectErr = &strava.Error{Code: strava.CodeRejected, Reason: "used code"}
	if rec = stravaRequest(t, h, "GET", "/api/strava/callback?code=c2&state="+nonce(state), state); rec.Header().Get("Location") != "/?strava=rejected" {
		t.Errorf("rejected: %s", rec.Header().Get("Location"))
	}
	sv.connectErr = &strava.Error{Code: strava.CodeMalformed, Reason: "no usable code"}
	if rec = stravaRequest(t, h, "GET", "/api/strava/callback?code=c2&state="+nonce(state), state); rec.Header().Get("Location") != "/?strava=rejected" {
		t.Errorf("malformed: %s", rec.Header().Get("Location"))
	}
	sv.connectErr = errors.New("dial tcp: refused")
	if rec = stravaRequest(t, h, "GET", "/api/strava/callback?code=c2&state="+nonce(state), state); rec.Header().Get("Location") != "/?strava=unavailable" {
		t.Errorf("down: %s", rec.Header().Get("Location"))
	}

	if rec = stravaRequest(t, h, "GET", "/api/me/strava"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"Rider usr_a"`) {
		t.Errorf("status: %d %s", rec.Code, rec.Body.String())
	}
	if rec = stravaRequest(t, h, "POST", "/api/me/strava/check"); rec.Code != 200 || !sv.checked {
		t.Errorf("check: %d", rec.Code)
	}
	if rec = stravaRequest(t, h, "DELETE", "/api/me/strava"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"never"`) {
		t.Errorf("disconnect: %d %s", rec.Code, rec.Body.String())
	}

	sv.err = errors.New("dial tcp: refused")
	if rec = stravaRequest(t, h, "GET", "/api/me/strava"); rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "dial") {
		t.Errorf("sidecar down: %d %s", rec.Code, rec.Body.String())
	}
	if rec = stravaRequest(t, h, "GET", "/api/me/strava/connect"); rec.Header().Get("Location") != "/?strava=unavailable" {
		t.Errorf("connect while down: %s", rec.Header().Get("Location"))
	}
	for code, want := range map[string]int{strava.CodeUpstream: 502, strava.CodeStorage: 500, strava.CodeConfig: 503, "odd": 503} {
		sv.err = &strava.Error{Code: code, Reason: "r"}
		if rec = stravaRequest(t, h, "GET", "/api/me/strava"); rec.Code != want || !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
			t.Errorf("%s: %d %s", code, rec.Code, rec.Body.String())
		}
	}
}

// Two listed users each connect their own account; one's callback never
// lands on the other's connection.
func TestStrava_eachListedUserHasTheirOwn(t *testing.T) {
	sv := &fakeStrava{}
	a := stravaServer(auth.User{ID: "usr_a"}, sv, "usr_a", "usr_b")
	b := stravaServer(auth.User{ID: "usr_b"}, sv, "usr_a", "usr_b")
	state := cookieNamed(stravaRequest(t, a, "GET", "/api/me/strava/connect"), stravaStateCookie)
	stravaRequest(t, a, "GET", "/api/strava/callback?code=code-a&state="+nonce(state), state)
	if rec := stravaRequest(t, b, "GET", "/api/me/strava"); !strings.Contains(rec.Body.String(), `"state":"never"`) {
		t.Errorf("b sees a's connection: %s", rec.Body.String())
	}
	stravaRequest(t, b, "DELETE", "/api/me/strava")
	if sv.conns["usr_a"] != "code-a" {
		t.Errorf("b's disconnect touched a: %v", sv.conns)
	}
	// A shared browser: a starts, b is signed in when Strava answers. a's
	// code must never become b's connection.
	state = cookieNamed(stravaRequest(t, a, "GET", "/api/me/strava/connect"), stravaStateCookie)
	rec := stravaRequest(t, b, "GET", "/api/strava/callback?code=code-a2&state="+nonce(state), state)
	if rec.Header().Get("Location") != "/?strava=state" || sv.conns["usr_b"] != "" {
		t.Errorf("a's code landed on b: %s, %v", rec.Header().Get("Location"), sv.conns)
	}
}

// A callback whose loom session ended meanwhile lands back in the app, not on
// a JSON 401.
func TestStrava_callbackWithoutSession(t *testing.T) {
	h := stravaServer(auth.User{ID: "usr_a"}, &fakeStrava{}, "usr_a")
	req := httptest.NewRequest("GET", "/api/strava/callback?code=c&state=s", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/?strava=signin" {
		t.Errorf("no session: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
