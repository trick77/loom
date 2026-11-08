// Package webpush sends Web Push messages (RFC 8030) to browsers: payload
// encryption (RFC 8291, aes128gcm) and VAPID (RFC 8292), on the standard
// library. webpush-go was the alternative; at ~460 stars it is under the bar
// for a new dependency, and the two RFCs are a few dozen lines.
package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// recordSize is the one aes128gcm record a message is sent in. maxPayload is
// what fits: the record minus the 16-byte tag and the delimiter octet.
const (
	recordSize = 4096
	maxPayload = recordSize - 16 - 1

	uncompressedPointLen = 65
)

// encrypt is RFC 8291 with the sender's ephemeral key and salt passed in, so
// the RFC's example can pin it.
func encrypt(uaPublic, authSecret, payload []byte, asPriv *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(authSecret) != 16 {
		return nil, fmt.Errorf("auth secret is %d bytes, want 16", len(authSecret))
	}
	if len(salt) != 16 {
		return nil, fmt.Errorf("salt is %d bytes, want 16", len(salt))
	}
	if len(payload) > maxPayload {
		return nil, fmt.Errorf("payload is %d bytes, at most %d fit", len(payload), maxPayload)
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	shared, err := asPriv.ECDH(ua)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}
	asPublic := asPriv.PublicKey().Bytes()
	info := make([]byte, 0, 14+2*65)
	info = append(append(append(info, "WebPush: info\x00"...), uaPublic...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(info), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, 21+len(asPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	// keyid is the uncompressed P-256 point, always 65 bytes.
	header = append(append(header, uncompressedPointLen), asPublic...)
	plain := append(append(make([]byte, 0, len(payload)+1), payload...), 0x02)
	return gcm.Seal(header, nonce, plain, nil), nil
}

// VAPID identifies loom to the push services (RFC 8292). A subscription is
// bound to the public key it was made with, so the key must outlive restarts.
type VAPID struct {
	priv    *ecdsa.PrivateKey
	public  string
	subject string
}

// DeriveVAPID derives the key from the session secret: no key to generate or
// keep, at the price that rotating the secret orphans every subscription (the
// browser renews its own on the next visit, see the UI's push sync). subject
// is the contact the push services see, an https URL or a mailto: address.
func DeriveVAPID(secret, subject string) (*VAPID, error) {
	if secret == "" {
		return nil, errors.New("vapid: empty secret")
	}
	if !strings.HasPrefix(subject, "https://") && !strings.HasPrefix(subject, "mailto:") {
		return nil, fmt.Errorf("vapid: subject %q must be an https or mailto URL", subject)
	}
	// A 32-byte string is a valid P-256 scalar but for a ~2^-32 chance; the
	// counter steps past such a value.
	for i := 0; i < 16; i++ {
		raw, err := hkdf.Key(sha256.New, []byte(secret), nil, "loom web push vapid "+strconv.Itoa(i), 32)
		if err != nil {
			return nil, err
		}
		priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
		if err != nil {
			continue
		}
		pub, err := priv.PublicKey.Bytes()
		if err != nil {
			return nil, err
		}
		return &VAPID{priv: priv, public: base64.RawURLEncoding.EncodeToString(pub), subject: subject}, nil
	}
	return nil, errors.New("vapid: no valid key derived")
}

// PublicKey is the applicationServerKey a browser subscribes with (base64url
// of the uncompressed point).
func (v *VAPID) PublicKey() string { return v.public }

// tokenLifetime keeps the JWT under the 24 h RFC 8292 allows.
const tokenLifetime = 12 * time.Hour

// authorization is the Authorization header for a message to endpoint.
func (v *VAPID) authorization(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(tokenLifetime).Unix(),
		"sub": v.subject,
	})
	if err != nil {
		return "", err
	}
	input := head + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, v.priv, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + input + "." + base64.RawURLEncoding.EncodeToString(sig) + ", k=" + v.public, nil
}

// pushHosts are the push services browsers subscribe with. The endpoint comes
// from the browser, so loom posts only to these: anything else would let a
// signed-in user point loom's requests at an internal address.
var pushHosts = []string{
	"fcm.googleapis.com",         // Chrome, Android
	".push.apple.com",            // Safari, iOS home-screen apps
	".push.services.mozilla.com", // Firefox
	".notify.windows.com",        // Edge
}

// fcmShard is Chrome's newer endpoint host (jmt17.google.com/fcm/send/…,
// seen 2026-11). Not all of .google.com: script.google.com would let a user
// point loom at their own Apps Script.
var fcmShard = regexp.MustCompile(`^jmt\d+\.google\.com$`)

// ValidEndpoint reports why loom would not post to endpoint, nil when it would.
func ValidEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return errors.New("endpoint must be a plain https URL")
	}
	if u.Path == "" || u.Path == "/" {
		return errors.New("endpoint has no path")
	}
	host := strings.ToLower(u.Hostname())
	if fcmShard.MatchString(host) {
		return nil
	}
	for _, h := range pushHosts {
		if host == h || (strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)) {
			return nil
		}
	}
	return fmt.Errorf("endpoint host %q is not a known push service", host)
}
