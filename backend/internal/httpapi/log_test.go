package httpapi

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestRedactErr_StripsQueryString(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://idp.example.com/token?client_secret=topsecret123", Err: errors.New("400 Bad Request")}

	got := redactErr(err)

	if strings.Contains(got.Error(), "topsecret123") {
		t.Fatalf("redactErr() leaked query string: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "idp.example.com/token") {
		t.Fatalf("redactErr() dropped host/path: %q", got.Error())
	}
}

// TestRedactErr_StripsQueryStringThroughWrapping is the regression test for the
// wrap-then-mutate trap: oidc.go wraps backend errors with fmt.Errorf("...: %w",
// err), which renders and freezes the message before any later mutation of the
// inner *url.Error could take effect. redactErr must scrub the rendered message,
// not mutate the inner struct.
func TestRedactErr_StripsQueryStringThroughWrapping(t *testing.T) {
	inner := &url.Error{Op: "Get", URL: "https://idp.example.com/token?client_secret=topsecret123&code=authcode456", Err: errors.New("400 Bad Request")}
	wrapped := fmt.Errorf("exchange oidc code: %w", inner)

	got := redactErr(wrapped)

	if strings.Contains(got.Error(), "topsecret123") {
		t.Fatalf("redactErr() leaked client_secret through wrapping: %q", got.Error())
	}
	if strings.Contains(got.Error(), "authcode456") {
		t.Fatalf("redactErr() leaked auth code through wrapping: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "idp.example.com/token") {
		t.Fatalf("redactErr() dropped host/path through wrapping: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "exchange oidc code") {
		t.Fatalf("redactErr() dropped wrap context: %q", got.Error())
	}
}

func TestRedactErr_StripsUserinfo(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://admin:hunter2@idp.example.com/token", Err: errors.New("dial failed")}

	got := redactErr(err)

	if strings.Contains(got.Error(), "hunter2") {
		t.Fatalf("redactErr() leaked userinfo password: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "idp.example.com/token") {
		t.Fatalf("redactErr() dropped host/path: %q", got.Error())
	}
}

func TestRedactErr_ReturnsPlainErrorsUnchanged(t *testing.T) {
	err := errors.New("boom")

	got := redactErr(err)

	if got != err {
		t.Fatalf("redactErr() = %v, want the original error unchanged", got)
	}
}

func TestRedactErr_ReturnsNilUnchanged(t *testing.T) {
	if got := redactErr(nil); got != nil {
		t.Fatalf("redactErr(nil) = %v, want nil", got)
	}
}
