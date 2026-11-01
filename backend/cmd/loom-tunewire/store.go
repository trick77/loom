package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/trick77/ship/kit/fsx"
)

const stateFile = "state.json"

// state is everything the sidecar keeps: the user's media-user-token, what the
// last call learned about it, and the cached developer token.
type state struct {
	UserToken     string    `json:"user_token,omitempty"`
	Storefront    string    `json:"storefront,omitempty"`
	SetAt         time.Time `json:"set_at,omitzero"`
	LastOKAt      time.Time `json:"last_ok_at,omitzero"`
	LastError     string    `json:"last_error,omitempty"`
	LastErrorKind string    `json:"last_error_kind,omitempty"`
	Expired       bool      `json:"expired,omitempty"`
	PlaylistCount *int      `json:"playlist_count,omitempty"`
	DevToken      string    `json:"dev_token,omitempty"`
	DevTokenExp   time.Time `json:"dev_token_exp,omitzero"`
}

// fileStore keeps state as one JSON file, 0600, replaced atomically (kit's
// fsx): a crash mid-write leaves the old file, never half a token.
type fileStore struct{ dir string }

func (s fileStore) load() (state, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return state{}, err
	}
	return st, nil
}

func (s fileStore) save(st state) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(filepath.Join(s.dir, stateFile), b, 0o600)
}
