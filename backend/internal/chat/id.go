package chat

import (
	"github.com/trick77/loom/internal/sqlutil"
)

// NewIDForInternalUse returns an opaque, unguessable identifier for internal use.
// Like NewShareID it uses 128-bit crypto/rand base64url encoding for randomness
// and unguessability; this variant is not exposed in URLs and can be safely used
// for any internal id that needs to be unforgeable.
func NewIDForInternalUse() string {
	return sqlutil.NewID()
}

// NewShareID returns the opaque public token used in a /share/<token> URL. It
// uses the same 128-bit crypto/rand base64url scheme as every other id, so the
// link is unguessable and un-enumerable — never a UUID or a guessable slug.
func NewShareID() string {
	return sqlutil.NewID()
}
