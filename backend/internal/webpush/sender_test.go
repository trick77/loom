package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return browser{key: key, auth: auth}
}

func (b browser) subscription(endpoint string) Subscription {
	return Subscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(b.key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(b.auth),
	}
}

func testSender(t *testing.T, srv *httptest.Server) *Sender {
	t.Helper()
	v, err := DeriveVAPID(strings.Repeat("k", 32), "https://loom.example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSender(v)
	s.client.Transport = srv.Client().Transport
	s.validate = func(string) error { return nil }
	return s
}

// A redirect could lead anywhere: loom never follows one.
func TestSendDoesNotFollowRedirects(t *testing.T) {
	followed := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed = true
			w.WriteHeader(http.StatusCreated)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	err := testSender(t, srv).Send(context.Background(), newBrowser(t).subscription(srv.URL+"/x"), Message{Payload: []byte("x")})
	if err == nil || followed {
		t.Errorf("err %v, followed %v", err, followed)
	}
}

func TestSendPostsAnEncryptedMessage(t *testing.T) {
	b := newBrowser(t)
	var got *http.Request
	var body []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must(io.ReadAll(r.Body))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	s := testSender(t, srv)

	if err := s.Send(context.Background(), b.subscription(srv.URL+"/push/abc"), Message{Payload: []byte(`{"title":"hi"}`)}); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/push/abc" {
		t.Errorf("%s %s", got.Method, got.URL.Path)
	}
	for header, want := range map[string]string{"Content-Encoding": "aes128gcm", "Ttl": "86400", "Urgency": "normal"} {
		if v := got.Header.Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	if auth := got.Header.Get("Authorization"); !strings.HasPrefix(auth, "vapid t=") || !strings.HasSuffix(auth, ", k="+s.vapid.PublicKey()) {
		t.Errorf("Authorization %q", auth)
	}
	if plain := decrypt(t, b.key, b.auth, body); string(plain) != `{"title":"hi"}` {
		t.Errorf("browser reads %q", plain)
	}
}

func TestSendReportsGoneAndFailure(t *testing.T) {
	b := newBrowser(t)
	for status, want := range map[int]error{
		http.StatusGone:     ErrGone,
		http.StatusNotFound: ErrGone,
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		err := testSender(t, srv).Send(context.Background(), b.subscription(srv.URL+"/x"), Message{Payload: []byte("x")})
		srv.Close()
		if !errors.Is(err, want) {
			t.Errorf("status %d: err %v, want %v", status, err, want)
		}
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
	}))
	defer srv.Close()
	err := testSender(t, srv).Send(context.Background(), b.subscription(srv.URL+"/x"), Message{Payload: []byte("x")})
	if err == nil || errors.Is(err, ErrGone) || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "BadJwtToken") {
		t.Errorf("403: err %v", err)
	}
}

func TestSendRefusesAnUnknownHost(t *testing.T) {
	v, _ := DeriveVAPID(strings.Repeat("k", 32), "https://loom.example.com")
	s := NewSender(v)
	called := false
	s.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("no")
	})}
	err := s.Send(context.Background(), newBrowser(t).subscription("https://169.254.169.254/latest"), Message{Payload: []byte("x")})
	if err == nil || called {
		t.Errorf("err %v, request made %v", err, called)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
