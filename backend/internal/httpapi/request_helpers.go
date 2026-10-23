package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/ship/kit/httpx"
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

// serverError is kit's logged 500 with the cause redacted first (query
// strings, userinfo): a cause that embeds an upstream URL may carry a key.
// Every 500 path must go through here so failures are never silent. The line
// carries the request id, which joins it to the access line and its masked
// path.
func serverError(w http.ResponseWriter, r *http.Request, err error, clientMessage string) {
	httpx.ServerError(w, r, redactErr(err), clientMessage)
}

// writeStoreError maps a chat store failure to its response: a
// chat.ValidationError is a 400 carrying its message, a missing thread or
// project is a 404, anything else is an internal error logged by serverError.
func writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *chat.ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, http.StatusBadRequest, validation.Msg)
	case errors.Is(err, chat.ErrThreadNotFound):
		httpx.WriteError(w, http.StatusNotFound, chat.ErrThreadNotFound.Error())
	case errors.Is(err, chat.ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, chat.ErrProjectNotFound.Error())
	default:
		serverError(w, r, err, "thread store failed")
	}
}

func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return auth.User{}, false
	}
	return user, true
}

func requireThreadStore(w http.ResponseWriter, s *server) bool {
	if s.thread == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "thread store is not configured")
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
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
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
