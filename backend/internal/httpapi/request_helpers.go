package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/auth"
)

// maxJSONBodyBytes bounds an ordinary JSON request body. Endpoints whose
// payload legitimately carries message-sized text use decodeJSONBodyLimit with
// a wider bound.
const maxJSONBodyBytes = 64 * 1024

// maxStreamBodyBytes bounds the two chat stream endpoints. One send may carry
// the content cap plus the pasted blocks that duplicate that text, and the
// incognito endpoint replays its whole transcript every turn, so the ordinary
// limit would reject legitimate sends long before the store's own caps apply.
const maxStreamBodyBytes = 4 << 20

// serverError logs the underlying cause of a 5xx with request context and
// returns a generic JSON error to the client (no internal details leak out).
// Every 500 path must go through here so failures are never silent. The cause
// is redacted for the log (query strings, userinfo) and the path drops a share
// token, since a cause that embeds an upstream URL may carry a key.
func serverError(w http.ResponseWriter, r *http.Request, err error, clientMessage string) {
	slog.Error("request failed",
		"method", r.Method,
		"path", logPath(r),
		"client_message", clientMessage,
		"err", redactErr(err),
	)
	writeJSONError(w, http.StatusInternalServerError, clientMessage)
}

func writeThreadStoreError(w http.ResponseWriter, r *http.Request, err error, validationStatus int, validationMessages ...string) {
	message := err.Error()
	for _, validationMessage := range validationMessages {
		if message == validationMessage {
			writeJSONError(w, validationStatus, message)
			return
		}
	}
	serverError(w, r, err, "thread store failed")
}

func writeMappedThreadStoreError(w http.ResponseWriter, r *http.Request, err error, statuses map[string]int) {
	message := err.Error()
	if status, ok := statuses[message]; ok {
		writeJSONError(w, status, message)
		return
	}
	serverError(w, r, err, "thread store failed")
}

func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return auth.User{}, false
	}
	return user, true
}

func requireThreadStore(w http.ResponseWriter, s *server) bool {
	if s.thread == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "thread store is not configured")
		return false
	}
	return true
}

// decodeJSONBody decodes one JSON value from a body bounded by maxJSONBodyBytes.
// Callers report a failure with writeDecodeError so an oversized body gets its
// own status.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	return decodeJSONBodyLimit(w, r, dst, maxJSONBodyBytes)
}

// decodeJSONBodyLimit is decodeJSONBody with an explicit byte bound.
func decodeJSONBodyLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	if r.Body == nil {
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request body must contain only one JSON value")
		}
		return err
	}
	return nil
}

// writeDecodeError maps a decodeJSONBody failure to its client status: an
// oversized body is a 413 the client can act on, anything else is a malformed
// payload.
func writeDecodeError(w http.ResponseWriter, err error) {
	if isRequestBodyTooLarge(err) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeJSONError(w, http.StatusBadRequest, "invalid request body")
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "request body too large")
}

func parseOptionalBool(r *http.Request, key string) (bool, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func parseOptionalLimit(r *http.Request, key string) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("%s must be between 1 and 1000", key)
	}
	return limit, nil
}
