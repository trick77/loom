package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/tunewire"
)

type fakeAppleMusic struct {
	token   string
	checked bool
	err     error
}

func (f *fakeAppleMusic) Status(_ context.Context, check bool) (tunewire.Status, error) {
	f.checked = check
	if f.err != nil {
		return tunewire.Status{}, f.err
	}
	if f.token == "" {
		return tunewire.Status{State: "never"}, nil
	}
	three := 3
	return tunewire.Status{State: "connected", Storefront: "ch", PlaylistCount: &three}, nil
}

func (f *fakeAppleMusic) SetToken(ctx context.Context, token string) (tunewire.Status, error) {
	switch token {
	case "bad":
		return tunewire.Status{}, &tunewire.Error{Reason: "Apple Music refused the token", Code: tunewire.CodeRejected}
	case "a;b":
		return tunewire.Status{}, &tunewire.Error{Reason: "that does not look like a media-user-token", Code: tunewire.CodeMalformed}
	case "down":
		return tunewire.Status{}, &tunewire.Error{Reason: "timeout", Code: tunewire.CodeUpstream}
	case "broken":
		return tunewire.Status{}, &tunewire.Error{Reason: "no bundle", Code: tunewire.CodeBridge}
	case "disk":
		return tunewire.Status{}, &tunewire.Error{Reason: "could not store", Code: tunewire.CodeStorage}
	case "mismatch":
		return tunewire.Status{}, &tunewire.Error{Reason: "token", Code: tunewire.CodeConfig}
	}
	f.token = token
	return f.Status(ctx, false)
}

func (f *fakeAppleMusic) ClearToken(ctx context.Context) (tunewire.Status, error) {
	f.token = ""
	return f.Status(ctx, false)
}

func appleMusicServer(user auth.User, am AppleMusicService, owner string) http.Handler {
	return New(Deps{
		Auth:            auth.NewMiddleware(fakeSessionStore{userID: user.ID}, fakeUserStore{user: user, ok: true}),
		Sessions:        fakeSessionStore{userID: user.ID},
		Users:           fakeUserStore{user: user, ok: true},
		AppleMusic:      am,
		AppleMusicUsers: []string{owner},
	})
}

func amRequest(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestAppleMusic_meFlag(t *testing.T) {
	owner := auth.User{ID: "usr_owner", Username: "jan", ResponseLanguage: "en"}
	for name, tc := range map[string]struct {
		am    AppleMusicService
		owner string
		want  bool
	}{
		"owner":       {&fakeAppleMusic{}, "usr_owner", true},
		"other user":  {&fakeAppleMusic{}, "usr_someone", false},
		"no sidecar":  {nil, "usr_owner", false},
		"empty owner": {&fakeAppleMusic{}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			h := appleMusicServer(owner, tc.am, tc.owner)
			for _, r := range [][3]string{{"GET", "/api/me", ""}, {"PATCH", "/api/me", `{"responseLanguage":"de"}`}} {
				code, body := amRequest(t, h, r[0], r[1], r[2])
				var me struct {
					ID         string `json:"id"`
					AppleMusic bool   `json:"appleMusic"`
				}
				if err := json.Unmarshal([]byte(body), &me); err != nil || code != http.StatusOK {
					t.Fatalf("%s: %d %s", r[0], code, body)
				}
				if me.ID != "usr_owner" || me.AppleMusic != tc.want {
					t.Errorf("%s: %+v, want appleMusic %v", r[0], me, tc.want)
				}
			}
		})
	}
}

func TestAppleMusic_otherUsersGet404(t *testing.T) {
	am := &fakeAppleMusic{token: "owner-token"}
	h := appleMusicServer(auth.User{ID: "usr_other"}, am, "usr_owner")
	for _, r := range [][3]string{
		{"GET", "/api/me/apple-music", ""},
		{"POST", "/api/me/apple-music/check", ""},
		{"PUT", "/api/me/apple-music", `{"token":"mine"}`},
		{"DELETE", "/api/me/apple-music", ""},
	} {
		if code, _ := amRequest(t, h, r[0], r[1], r[2]); code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", r[0], r[1], code)
		}
	}
	if am.token != "owner-token" {
		t.Errorf("another user changed the token to %q", am.token)
	}
}

func TestAppleMusic_ownerFlow(t *testing.T) {
	am := &fakeAppleMusic{}
	h := appleMusicServer(auth.User{ID: "usr_owner"}, am, "usr_owner")

	if code, body := amRequest(t, h, "GET", "/api/me/apple-music", ""); code != 200 || !strings.Contains(body, `"state":"never"`) {
		t.Errorf("status: %d %s", code, body)
	}
	code, body := amRequest(t, h, "PUT", "/api/me/apple-music", `{"token":"secret-cookie"}`)
	if code != 200 || !strings.Contains(body, `"state":"connected"`) || strings.Contains(body, "secret-cookie") {
		t.Errorf("set: %d %s", code, body)
	}
	if am.token != "secret-cookie" {
		t.Errorf("token not passed through: %q", am.token)
	}
	if code, body := amRequest(t, h, "PUT", "/api/me/apple-music", `{"token":"bad"}`); code != http.StatusUnprocessableEntity || !strings.Contains(body, `"code":"rejected"`) {
		t.Errorf("rejected: %d %s", code, body)
	}
	if code, body := amRequest(t, h, "PUT", "/api/me/apple-music", `{"token":"a;b"}`); code != http.StatusUnprocessableEntity || !strings.Contains(body, `"code":"malformed"`) {
		t.Errorf("malformed: %d %s", code, body)
	}
	for token, want := range map[string]struct {
		status int
		code   string
	}{
		"down":     {http.StatusBadGateway, tunewire.CodeUpstream},
		"broken":   {http.StatusBadGateway, tunewire.CodeBridge},
		"disk":     {http.StatusInternalServerError, tunewire.CodeStorage},
		"mismatch": {http.StatusServiceUnavailable, tunewire.CodeConfig},
	} {
		code, body := amRequest(t, h, "PUT", "/api/me/apple-music", `{"token":"`+token+`"}`)
		if code != want.status || !strings.Contains(body, `"code":"`+want.code+`"`) {
			t.Errorf("%s: %d %s", token, code, body)
		}
	}
	if code, _ := amRequest(t, h, "PUT", "/api/me/apple-music", `{"tok":"x"}`); code != http.StatusBadRequest {
		t.Errorf("bad body: %d", code)
	}
	if code, _ := amRequest(t, h, "PUT", "/api/me/apple-music", `{"token":"`+strings.Repeat("x", maxAppleMusicTokenBody)+`"}`); code != http.StatusRequestEntityTooLarge {
		t.Errorf("big body: %d", code)
	}
	if code, _ := amRequest(t, h, "POST", "/api/me/apple-music/check", ""); code != 200 || !am.checked {
		t.Errorf("check: %d %v", code, am.checked)
	}
	if code, body := amRequest(t, h, "DELETE", "/api/me/apple-music", ""); code != 200 || !strings.Contains(body, `"state":"never"`) {
		t.Errorf("clear: %d %s", code, body)
	}
	am.err = errors.New("dial tcp: connection refused")
	if code, body := amRequest(t, h, "GET", "/api/me/apple-music", ""); code != http.StatusServiceUnavailable || strings.Contains(body, "dial") {
		t.Errorf("sidecar down: %d %s", code, body)
	}
}
