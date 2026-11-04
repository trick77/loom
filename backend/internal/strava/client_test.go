package strava

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/loomuser"
)

func TestClient(t *testing.T) {
	var lastBody, lastAuth, lastPath, lastUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody, lastAuth, lastPath = string(b), r.Header.Get("Authorization"), r.Method+" "+r.URL.RequestURI()
		lastUser = r.Header.Get(loomuser.Header)
		switch {
		case r.URL.Path == "/authorize" && r.URL.Query().Get("state") == "evil":
			_, _ = fmt.Fprint(w, `{"url":"javascript:alert(1)"}`)
		case r.URL.Path == "/authorize":
			_, _ = fmt.Fprintf(w, `{"url":"https://www.strava.com/oauth/authorize?state=%s"}`, r.URL.Query().Get("state"))
		case r.Method == http.MethodPut && strings.Contains(lastBody, "used"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"Strava refused the code","code":"rejected"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "spaces"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"no usable code","code":"malformed"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "down"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, `{"error":"strava timeout","code":"upstream"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "disk"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"error":"could not store","code":"storage"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "garbage"):
			_, _ = fmt.Fprint(w, `{`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "teapot"):
			w.WriteHeader(http.StatusTeapot)
		case r.Method == http.MethodDelete:
			_, _ = fmt.Fprint(w, `{"state":"never"}`)
		default:
			_, _ = fmt.Fprint(w, `{"state":"connected","athlete":{"id":42,"name":"Jan"},"scopes":["read_all"],"missingScopes":["activity:read_all"]}`)
		}
	}))
	defer srv.Close()
	c := New(srv.URL+"/", "tok")
	ctx := context.Background()

	st, err := c.Status(ctx, "usr_1", false)
	if err != nil || st.State != "connected" || st.Athlete == nil || st.Athlete.Name != "Jan" || len(st.MissingScopes) != 1 {
		t.Fatalf("status: %+v %v", st, err)
	}
	if lastAuth != "Bearer tok" || lastPath != "GET /status" || lastUser != "usr_1" {
		t.Errorf("request: %s %s user %q", lastAuth, lastPath, lastUser)
	}
	if _, _ = c.Status(ctx, "usr_1", true); lastPath != "GET /status?check=1" {
		t.Errorf("check path: %s", lastPath)
	}
	u, err := c.AuthorizeURL(ctx, "usr_2", "https://loom.example/api/strava/callback", "s1")
	if err != nil || u != "https://www.strava.com/oauth/authorize?state=s1" || lastUser != "usr_2" ||
		!strings.Contains(lastPath, "redirect_uri=https%3A%2F%2Floom.example%2Fapi%2Fstrava%2Fcallback") {
		t.Errorf("authorize: %q %v %s", u, err, lastPath)
	}
	if _, err := c.AuthorizeURL(ctx, "usr_2", "https://loom.example/cb", "evil"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a non-http authorize URL got through: %v", err)
	}
	if st, err := c.Connect(ctx, "usr_3", "code-1", "read_all"); err != nil || st.State != "connected" || lastBody != `{"code":"code-1","scope":"read_all"}` || lastUser != "usr_3" {
		t.Errorf("connect: %+v %v %s", st, err, lastBody)
	}
	for code, want := range map[string]string{"used": CodeRejected, "spaces": CodeMalformed, "down": CodeUpstream, "disk": CodeStorage} {
		var e *Error
		if _, err := c.Connect(ctx, "usr_1", code, ""); !errors.As(err, &e) || e.Code != want || e.Reason == "" {
			t.Errorf("%s: %v, want %s", code, err, want)
		}
	}
	if _, err := c.Connect(ctx, "usr_1", "disk", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("storage is not unavailable: %v", err)
	}
	if _, err := c.Connect(ctx, "usr_1", "used", ""); errors.Is(err, ErrUnavailable) {
		t.Errorf("a rejection counted as unavailable: %v", err)
	}
	for _, code := range []string{"garbage", "teapot"} {
		if _, err := c.Connect(ctx, "usr_1", code, ""); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: %v", code, err)
		}
	}
	if st, err := c.Disconnect(ctx, "usr_4"); err != nil || st.State != "never" || lastPath != "DELETE /connection" || lastUser != "usr_4" {
		t.Errorf("disconnect: %+v %v", st, err)
	}
	lastPath = ""
	if _, err := c.Status(ctx, "", false); !errors.Is(err, ErrUnavailable) || lastPath != "" {
		t.Errorf("no user: %v, reached %q", err, lastPath)
	}
}

func TestClient_unavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	var e *Error
	if _, err := New(srv.URL, "x").Status(context.Background(), "usr_1", false); !errors.As(err, &e) || e.Code != CodeConfig || !errors.Is(err, ErrUnavailable) {
		t.Errorf("401: %v", err)
	}
	srv.Close()
	if _, err := New(srv.URL, "x").Disconnect(context.Background(), "usr_1"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("down: %v", err)
	}
	if (&Error{Name: "strava", Code: CodeRejected, Reason: "r"}).Error() != "strava: rejected: r" {
		t.Error("error text")
	}
}
