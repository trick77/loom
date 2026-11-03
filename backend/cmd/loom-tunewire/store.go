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

// account is one loom user's Apple Music connection: the user's own
// media-user-token and what the last call learned about it. Never read for
// anyone else.
type account struct {
	UserToken     string    `json:"user_token"`
	Storefront    string    `json:"storefront,omitempty"`
	SetAt         time.Time `json:"set_at,omitzero"`
	LastOKAt      time.Time `json:"last_ok_at,omitzero"`
	LastError     string    `json:"last_error,omitempty"`
	LastErrorKind string    `json:"last_error_kind,omitempty"`
	Expired       bool      `json:"expired,omitempty"`
	PlaylistCount *int      `json:"playlist_count,omitempty"`
}

// state is everything the sidecar keeps: one account per loom user id, and
// the cached developer token, which is the web player's and no one's data.
// A file from before per-user accounts (a top-level user_token) loads with no
// accounts: its token belonged to whoever pasted it, so every user connects
// their own.
type state struct {
	Users       map[string]account `json:"users,omitempty"`
	DevToken    string             `json:"dev_token,omitempty"`
	DevTokenExp time.Time          `json:"dev_token_exp,omitzero"`
	// LegacyToken is the old file's single shared token, read only to be
	// dropped: load clears it and rewrites the file at once.
	LegacyToken string `json:"user_token,omitempty"`
}

// clone copies st, accounts included, so a change built on it never touches
// the live state before it is saved.
func (st state) clone() state {
	out := st
	out.Users = make(map[string]account, len(st.Users))
	for id, a := range st.Users {
		out.Users[id] = a
	}
	return out
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
	if st.LegacyToken != "" {
		st.LegacyToken = ""
		if err := s.save(st); err != nil {
			return state{}, err
		}
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
