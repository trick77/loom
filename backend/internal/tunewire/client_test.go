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
)

func TestClient(t *testing.T) {
	var lastBody, lastAuth, lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody, lastAuth, lastPath = string(b), r.Header.Get("Authorization"), r.Method+" "+r.URL.RequestURI()
		switch {
		case r.Method == http.MethodPut && strings.Contains(lastBody, "bad"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"Apple Music refused the token","code":"rejected"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "a;b"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"that does not look like a media-user-token","code":"malformed"}`)
		case r.Method == http.MethodPut && strings.Contains(lastBody, "down"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, `{"error":"could not reach Apple Music: timeout"}`)
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

	st, err := c.Status(ctx, false)
	if err != nil || st.State != "connected" || st.PlaylistCount == nil || *st.PlaylistCount != 7 || st.LastOKAt.IsZero() {
		t.Fatalf("status: %+v %v", st, err)
	}
	if lastAuth != "Bearer tok" || lastPath != "GET /status" {
		t.Errorf("request: %s %s", lastAuth, lastPath)
	}
	if _, _ = c.Status(ctx, true); lastPath != "GET /status?check=1" {
		t.Errorf("check path: %s", lastPath)
	}
	if st, err := c.SetToken(ctx, "good"); err != nil || st.State != "connected" || lastBody != `{"token":"good"}` {
		t.Errorf("set: %+v %v %s", st, err, lastBody)
	}
	var rej *RejectedError
	if _, err := c.SetToken(ctx, "bad"); !errors.As(err, &rej) || rej.Reason != "Apple Music refused the token" || rej.Code != CodeRejected {
		t.Errorf("rejected: %v", err)
	}
	if _, err := c.SetToken(ctx, "a;b"); !errors.As(err, &rej) || rej.Code != CodeMalformed {
		t.Errorf("malformed: %v", err)
	}
	var up *UpstreamError
	if _, err := c.SetToken(ctx, "down"); !errors.As(err, &up) {
		t.Errorf("upstream: %v", err)
	}
	if _, err := c.SetToken(ctx, "garbage"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("bad json: %v", err)
	}
	if st, err := c.ClearToken(ctx); err != nil || st.State != "never" || lastPath != "DELETE /token" {
		t.Errorf("clear: %+v %v", st, err)
	}
}

func TestClient_unavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	if _, err := New(srv.URL, "x").Status(context.Background(), false); !errors.Is(err, ErrUnavailable) {
		t.Errorf("401: %v", err)
	}
	srv.Close()
	if _, err := New(srv.URL, "x").Status(context.Background(), false); !errors.Is(err, ErrUnavailable) {
		t.Errorf("down: %v", err)
	}
	if (&RejectedError{Reason: "r"}).Error() == "" || (&UpstreamError{Reason: "u"}).Error() == "" {
		t.Error("empty error text")
	}
}
