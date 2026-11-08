package webpush

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Subscription is what a browser's PushSubscription.toJSON() carries; the
// keys are base64url.
//
// Table: push_subscriptions
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

// Stored is a saved subscription.
//
// Table: push_subscriptions
type Stored struct {
	Subscription
	ID            string
	UserAgent     string
	VAPIDKey      string
	LastSuccessAt *time.Time
}
