package webpush

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrGone is a push service saying the subscription no longer exists (404,
// 410): the browser unsubscribed or the user revoked the permission.
var ErrGone = errors.New("push subscription gone")

// Subscription is what a browser's PushSubscription.toJSON() carries; the
// keys are base64url.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// Validate reports why loom could not push to s: an endpoint outside the push
// services, or keys that are no P-256 point and 16-byte secret.
func (s Subscription) Validate() error {
	if err := ValidEndpoint(s.Endpoint); err != nil {
		return err
	}
	_, _, err := s.keys()
	return err
}

// keys decodes the browser's public key and auth secret (base64url, padded
// or not) and checks their shape.
func (s Subscription) keys() (uaPublic, authSecret []byte, err error) {
	uaPublic, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(s.P256dh, "="))
	if err != nil {
		return nil, nil, fmt.Errorf("p256dh: %w", err)
	}
	if _, err := ecdh.P256().NewPublicKey(uaPublic); err != nil {
		return nil, nil, fmt.Errorf("p256dh: %w", err)
	}
	authSecret, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Auth, "="))
	if err != nil {
		return nil, nil, fmt.Errorf("auth: %w", err)
	}
	if len(authSecret) != 16 {
		return nil, nil, fmt.Errorf("auth is %d bytes, want 16", len(authSecret))
	}
	return uaPublic, authSecret, nil
}

// Message is one push. TTL is how long the push service holds it for an
// offline device (zero: a day).
type Message struct {
	Payload []byte
	TTL     time.Duration
}

const (
	defaultTTL  = 24 * time.Hour
	sendTimeout = 15 * time.Second
)

// Sender posts messages to push services.
type Sender struct {
	vapid    *VAPID
	client   *http.Client
	now      func() time.Time
	validate func(string) error
}

// NewSender signs with v. Its client follows no redirect: the endpoint was
// checked, a redirect target was not.
func NewSender(v *VAPID) *Sender {
	return &Sender{
		vapid: v,
		client: &http.Client{
			Timeout:       sendTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now:      time.Now,
		validate: ValidEndpoint,
	}
}

// PublicKey is the key browsers subscribe with.
func (s *Sender) PublicKey() string { return s.vapid.PublicKey() }

// Send encrypts msg for sub and posts it.
func (s *Sender) Send(ctx context.Context, sub Subscription, msg Message) error {
	if err := s.validate(sub.Endpoint); err != nil {
		return err
	}
	uaPublic, authSecret, err := sub.keys()
	if err != nil {
		return err
	}
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	body, err := encrypt(uaPublic, authSecret, msg.Payload, asPriv, salt)
	if err != nil {
		return err
	}
	authorization, err := s.vapid.authorization(sub.Endpoint, s.now())
	if err != nil {
		return err
	}
	ttl := msg.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(ttl/time.Second)))
	req.Header.Set("Urgency", "normal")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return ErrGone
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	default:
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("push service answered %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
}
