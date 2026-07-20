package httpapi

import (
	"errors"
	"regexp"
)

// queryStringRe matches a "?" and everything after it up to whitespace or a
// closing quote, so it strips the whole query string of any URL embedded in
// an error message — not an allowlist of parameter names, which would leak
// the next time a library adds a differently-named secret parameter.
var queryStringRe = regexp.MustCompile(`(\?)[^\s"]+`)

// userinfoRe matches "user:password@" (or "user@") userinfo following a
// "scheme://" prefix, so it can be stripped while leaving the host and path
// visible.
var userinfoRe = regexp.MustCompile(`(://)[^\s"/@]+@`)

// redactErr returns an error whose message has had URL query strings and
// userinfo (user:password@) stripped, safe to attach to a log record. It
// operates on the fully rendered error message (err.Error()) rather than on
// a typed *url.Error, because it must survive fmt.Errorf("...: %w", err)
// wrapping: fmt.Errorf renders and freezes the message at wrap time, so
// mutating an inner *url.Error afterward would have no effect on what
// Error() returns for the wrapping error.
//
// When nothing is redacted, the original err is returned unchanged so the
// error chain (errors.Is/errors.As) stays intact. When redaction does
// change the message, the returned error is a plain errors.New(msg), which
// breaks that chain. That is acceptable only because the result of
// redactErr is used solely as a log attribute value — never in an
// errors.Is/errors.As check. Do not use it for control flow.
func redactErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	redacted := queryStringRe.ReplaceAllString(msg, "$1[redacted]")
	redacted = userinfoRe.ReplaceAllString(redacted, "$1[redacted]@")
	if redacted == msg {
		return err
	}
	return errors.New(redacted)
}
