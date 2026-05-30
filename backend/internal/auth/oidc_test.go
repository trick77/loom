package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestOIDCService_CallbackRejectsInvalidState(t *testing.T) {
	service := NewOIDCService(OIDCServiceConfig{
		ClientID: "client",
		Backend: fakeOIDCBackend{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state=bad&code=abc", nil)

	_, err := service.HandleCallback(req)
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error = %v, want ErrInvalidState", err)
	}
}

func TestOIDCService_CallbackMapsVerifiedClaims(t *testing.T) {
	service := NewOIDCService(OIDCServiceConfig{
		ClientID: "client",
		Backend: fakeOIDCBackend{claims: Claims{
			Subject:  "sub-1",
			Username: "jan",
			Email:    "jan@example.com",
			Groups:   []string{"spark-admins"},
		}, nonce: "valid-nonce"},
	})
	req := requestWithValidStateAndNonce(t, service)

	claims, err := service.HandleCallback(req)
	if err != nil {
		t.Fatalf("HandleCallback() error: %v", err)
	}
	if claims.Subject != "sub-1" {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if claims.Groups[0] != "spark-admins" {
		t.Fatalf("groups = %v", claims.Groups)
	}
}

func TestOIDCService_CallbackRejectsMissingNonceClaim(t *testing.T) {
	service := NewOIDCService(OIDCServiceConfig{
		ClientID: "client",
		Backend: fakeOIDCBackend{claims: Claims{Subject: "sub-1"}},
	})
	req := requestWithValidStateAndNonce(t, service)

	_, err := service.HandleCallback(req)
	if !errors.Is(err, ErrInvalidNonce) {
		t.Fatalf("error = %v, want ErrInvalidNonce", err)
	}
}

func TestOIDCService_CallbackRejectsNonceMismatch(t *testing.T) {
	service := NewOIDCService(OIDCServiceConfig{
		ClientID: "client",
		Backend: fakeOIDCBackend{claims: Claims{Subject: "sub-1"}, nonce: "wrong-nonce"},
	})
	req := requestWithValidStateAndNonce(t, service)

	_, err := service.HandleCallback(req)
	if !errors.Is(err, ErrInvalidNonce) {
		t.Fatalf("error = %v, want ErrInvalidNonce", err)
	}
}

func TestOIDCService_LoginSetsStateAndNonceCookies(t *testing.T) {
	service := NewOIDCService(OIDCServiceConfig{
		ClientID:    "client",
		RedirectURL: "https://spark.example.com/api/auth/callback",
		Backend:    fakeOIDCBackend{authURL: "https://auth.example.com/authorize"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	rec := httptest.NewRecorder()

	service.StartLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "https://auth.example.com/authorize") {
		t.Fatalf("Location = %q", loc)
	}
	if len(rec.Result().Cookies()) != 2 {
		t.Fatalf("cookies = %d, want state and nonce", len(rec.Result().Cookies()))
	}
}

func requestWithValidStateAndNonce(t *testing.T, service *OIDCService) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	service.StartLogin(rec, req)

	var state string
	callback := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc", nil)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == oidcStateCookieName {
			state = cookie.Value
		}
		if cookie.Name == oidcNonceCookieName {
			cookie.Value = "valid-nonce"
		}
		callback.AddCookie(cookie)
	}
	callback.URL.RawQuery = "code=abc&state=" + state
	return callback
}

type fakeOIDCBackend struct {
	authURL string
	claims  Claims
	nonce   string
}

func (f fakeOIDCBackend) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	if f.authURL != "" {
		return f.authURL + "?state=" + state
	}
	return "https://auth.example.com/authorize?state=" + state
}

func (f fakeOIDCBackend) Exchange(context.Context, string) (*oauth2.Token, error) {
	return &oauth2.Token{}, nil
}

func (f fakeOIDCBackend) VerifyClaims(context.Context, *oauth2.Token) (VerifiedClaims, error) {
	return VerifiedClaims{Claims: f.claims, Nonce: f.nonce}, nil
}
