package tunewire

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
		case r.Method == http.MethodPut && strings.Contains(lastBody, "bad"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"Apple Music refused the token","code":"rejected"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "a;b"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"that does not look like a media-user-token","code":"malformed"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "down"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, `{"error":"could not check the token: timeout","code":"upstream"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "broken"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, `{"error":"no bundle","code":"bridge"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "disk"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"error":"could not store","code":"storage"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "garbage"):
			_, _ = fmt.Fprint(w, `{`)
		case r.Method == http.MethodDelete:
			_, _ = fmt.Fprint(w, `{"state":"never","playlistCount":0}`)
		default:
			_, _ = fmt.Fprint(w, `{"state":"connected","storefront":"ch","playlistCount":7,"lastOkAt":"2026-11-01T10:00:00Z"}`)
		}
	}))
	defer srv.Close()
	c := New(srv.URL+"/", "tok")
	ctx := context.Background()

	st, err := c.Status(ctx, "usr_1", false)
	if err != nil || st.State != "connected" || st.PlaylistCount == nil || *st.PlaylistCount != 7 || st.LastOKAt.IsZero() {
		t.Fatalf("status: %+v %v", st, err)
	}
	if lastAuth != "Bearer tok" || lastPath != "GET /status" || lastUser != "usr_1" {
		t.Errorf("request: %s %s user %q", lastAuth, lastPath, lastUser)
	}
	if _, _ = c.Status(ctx, "usr_1", true); lastPath != "GET /status?check=1" {
		t.Errorf("check path: %s", lastPath)
	}
	if st, err := c.SetToken(ctx, "usr_2", "good"); err != nil || st.State != "connected" || lastBody != `{"token":"good"}` || lastUser != "usr_2" {
		t.Errorf("set: %+v %v %s user %q", st, err, lastBody, lastUser)
	}
	for token, want := range map[string]string{
		"bad": CodeRejected, "a;b": CodeMalformed, "down": CodeUpstream, "broken": CodeBridge, "disk": CodeStorage,
	} {
		var e *Error
		if _, err := c.SetToken(ctx, "usr_1", token); !errors.As(err, &e) || e.Code != want || e.Reason == "" {
			t.Errorf("%s: %v, want code %s", token, err, want)
		}
	}
	if _, err := c.SetToken(ctx, "usr_1", "disk"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("storage is not unavailable: %v", err)
	}
	if _, err := c.SetToken(ctx, "usr_1", "bad"); errors.Is(err, ErrUnavailable) {
		t.Errorf("a rejection counted as unavailable: %v", err)
	}
	if _, err := c.SetToken(ctx, "usr_1", "garbage"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("bad json: %v", err)
	}
	if st, err := c.ClearToken(ctx, "usr_3"); err != nil || st.State != "never" || lastPath != "DELETE /token" || lastUser != "usr_3" {
		t.Errorf("clear: %+v %v user %q", st, err, lastUser)
	}
	// A call for nobody never leaves loom.
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
	if _, err := New(srv.URL, "x").Status(context.Background(), "usr_1", false); !errors.Is(err, ErrUnavailable) {
		t.Errorf("down: %v", err)
	}
	if (&Error{Code: CodeRejected, Reason: "r"}).Error() == "" {
		t.Error("empty error text")
	}
}
