package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/trick77/slop/internal/auth"
)

const maxJSONBodyBytes = 64 * 1024

func writeChatStoreError(w http.ResponseWriter, err error, validationStatus int, validationMessages ...string) {
	message := err.Error()
	for _, validationMessage := range validationMessages {
		if message == validationMessage {
			writeJSONError(w, validationStatus, message)
			return
		}
	}
	writeJSONError(w, http.StatusInternalServerError, "chat store failed")
}

func writeMappedChatStoreError(w http.ResponseWriter, err error, statuses map[string]int) {
	message := err.Error()
	if status, ok := statuses[message]; ok {
		writeJSONError(w, status, message)
		return
	}
	writeJSONError(w, http.StatusInternalServerError, "chat store failed")
}

func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return auth.User{}, false
	}
	return user, true
}

func requireChat(w http.ResponseWriter, s *server) bool {
	if s.chat == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "chat is not configured")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
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
