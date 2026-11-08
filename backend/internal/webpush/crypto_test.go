package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()
	out, err := base64.RawURLEncoding.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return out
}

// RFC 8291 Appendix A: the example's fixed keys and salt give its exact message.
func TestEncryptMatchesRFC8291Example(t *testing.T) {
	asPriv, err := ecdh.P256().NewPrivateKey(b64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	uaPublic := b64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcx aOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	authSecret := b64(t, "BTBZMqHH6r4Tts7J_aSIgg")
	salt := b64(t, "DGv6ra1nlYgDCS1FRnbzlw")

	got, err := encrypt(uaPublic, authSecret, []byte("When I grow up, I want to be a watermelon"), asPriv, salt)
	if err != nil {
		t.Fatal(err)
	}
	header := b64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z 9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml mlMoZIIgDll6e3vCYLocInmYWAmS6Tlz AC8wEqKK6PBru3jl7A8")
	ciphertext := b64(t, "8pfeW0KbunFT06SuDKoJH9Ql87S1QUrd irN6GcG7sFz1y1sqLgVi1VhjVkHsUoEs bI_0LpXMuGvnzQ")
	if want := append(header, ciphertext...); !bytes.Equal(got, want) {
		t.Fatalf("message differs from RFC 8291\n got %x\nwant %x", got, want)
	}
}

func TestEncryptRefusesBadKeys(t *testing.T) {
	asPriv, _ := ecdh.P256().GenerateKey(nil)
	if _, err := encrypt([]byte{4, 1, 2}, make([]byte, 16), []byte("x"), asPriv, make([]byte, 16)); err == nil {
		t.Error("short p256dh accepted")
	}
	ua, _ := ecdh.P256().GenerateKey(nil)
	if _, err := encrypt(ua.PublicKey().Bytes(), make([]byte, 8), []byte("x"), asPriv, make([]byte, 16)); err == nil {
		t.Error("short auth secret accepted")
	}
	if _, err := encrypt(ua.PublicKey().Bytes(), make([]byte, 16), make([]byte, maxPayload+1), asPriv, make([]byte, 16)); err == nil {
		t.Error("payload past one record accepted")
	}
}

// decrypt is the browser's side of RFC 8291, for the tests.
func decrypt(t *testing.T, ua *ecdh.PrivateKey, authSecret, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body too short: %d", len(body))
	}
	salt, rs, idLen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != recordSize {
		t.Fatalf("record size %d", rs)
	}
	asPublicBytes := body[21 : 21+idLen]
	asPublic, err := ecdh.P256().NewPublicKey(asPublicBytes)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := ua.ECDH(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPublicBytes...)
	ikm, _ := hkdf.Key(sha256.New, shared, authSecret, string(info), 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idLen:], nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatalf("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func TestDeriveVAPIDIsStableAndSecretBound(t *testing.T) {
	a, err := DeriveVAPID(strings.Repeat("a", 32), "https://loom.example.com")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := DeriveVAPID(strings.Repeat("a", 32), "https://loom.example.com")
	other, _ := DeriveVAPID(strings.Repeat("b", 32), "https://loom.example.com")
	if a.PublicKey() != again.PublicKey() {
		t.Error("same secret derived a different key")
	}
	if a.PublicKey() == other.PublicKey() {
		t.Error("different secrets derived the same key")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a.PublicKey())
	if err != nil || len(raw) != 65 || raw[0] != 4 {
		t.Fatalf("public key is not an uncompressed P-256 point: %x (%v)", raw, err)
	}
	if _, err := DeriveVAPID("", "https://loom.example.com"); err == nil {
		t.Error("empty secret accepted")
	}
	if _, err := DeriveVAPID(strings.Repeat("a", 32), "ftp://x"); err == nil {
		t.Error("subject that is neither https nor mailto accepted")
	}
}

func TestVAPIDAuthorizationVerifies(t *testing.T) {
	v, err := DeriveVAPID(strings.Repeat("s", 40), "https://loom.example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	header, err := v.authorization("https://web.push.apple.com/QGx1c3Q", now)
	if err != nil {
		t.Fatal(err)
	}
	rest, ok := strings.CutPrefix(header, "vapid t=")
	if !ok {
		t.Fatalf("header %q", header)
	}
	token, key, ok := strings.Cut(rest, ", k=")
	if !ok || key != v.PublicKey() {
		t.Fatalf("header %q lacks k=<public key>", header)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", token)
	}
	var head map[string]string
	_ = json.Unmarshal(b64(t, parts[0]), &head)
	if head["alg"] != "ES256" || head["typ"] != "JWT" {
		t.Errorf("jwt header %v", head)
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(b64(t, parts[1]), &claims)
	if claims.Aud != "https://web.push.apple.com" || claims.Sub != "https://loom.example.com" {
		t.Errorf("claims %+v", claims)
	}
	if exp := time.Unix(claims.Exp, 0); !exp.After(now) || exp.Sub(now) > 24*time.Hour {
		t.Errorf("exp %v not within 24h of %v", exp, now)
	}
	sig := b64(t, parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want raw r||s", len(sig))
	}
	pubBytes, _ := base64.RawURLEncoding.DecodeString(v.PublicKey())
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pubBytes)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		t.Error("signature does not verify with the public key")
	}
}

func TestValidEndpoint(t *testing.T) {
	for _, ok := range []string{
		"https://web.push.apple.com/QGx1c3Q",
		"https://fcm.googleapis.com/fcm/send/abc:def",
		"https://jmt17.google.com/fcm/send/eDGcPD9F67c:APA91b",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAA",
		"https://wns2-par02p.notify.windows.com/w/?token=x",
	} {
		if err := ValidEndpoint(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"",
		"http://web.push.apple.com/x",
		"https://evilpush.apple.com/x",
		"https://push.apple.com.evil.example/x",
		"https://127.0.0.1/x",
		"https://web.push.apple.com:8443/x",
		"https://user@web.push.apple.com/x",
		"https://loom.example.com/x",
		"https://fcm.googleapis.com",
		"https://google.com.evil.example/x",
		"https://notgoogle.com/x",
		"https://script.google.com/macros/s/abc/exec",
		"https://jmt17.google.com.evil.example/x",
		"https://xjmt17.google.com/x",
	} {
		if err := ValidEndpoint(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
