// Package sqlutil holds the small helpers every SQLite-backed store needs:
// parsing the timestamps SQLite writes, escaping LIKE patterns, and minting
// ids. They lived as private copies in chat, artifact and auth before.
package sqlutil

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// ParseTime parses a timestamp column. SQLite's datetime('now') writes
// "2006-01-02 15:04:05" in UTC; RFC 3339 forms are accepted for values the
// application wrote itself. The result is always in UTC.
func ParseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.DateTime, time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}

// EscapeLike escapes a term for use inside a LIKE pattern with ESCAPE '\'.
func EscapeLike(term string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(term)
}

// NewID returns an opaque, unguessable 128-bit id as 22 base64url characters.
// Every row id and every public token (share links) uses this scheme, so no
// id is enumerable.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
