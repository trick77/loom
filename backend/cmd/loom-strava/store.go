package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/trick77/ship/kit/fsx"
)

const (
	stateFile  = "state.json"
	metricsDir = "metrics"
)

// userIDRe is what a loom user id may look like before it names a file or a
// map key here: loom's ids are UUID-like, and anything else (a path, a dot,
// a slash) never reaches the disk.
var userIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validUserID(id string) bool { return userIDRe.MatchString(id) }

// state is everything the sidecar keeps besides computed metrics: one Strava
// connection per loom user id.
type state struct {
	Users map[string]connection `json:"users"`
}

// connection is one loom user's own Strava account. Values, never pointers:
// a copy handed out cannot change the stored one.
type connection struct {
	AthleteID    int64     `json:"athlete_id"`
	AthleteName  string    `json:"athlete_name"`
	Scope        string    `json:"scope"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    int64     `json:"expires_at"` // unix seconds, as Strava sends it
	ConnectedAt  time.Time `json:"connected_at"`
	LastOKAt     time.Time `json:"last_ok_at,omitzero"`
	LastError    string    `json:"last_error,omitempty"`
	Expired      bool      `json:"expired,omitempty"`
}

// fileStore keeps state as one JSON file and each user's metrics as one file,
// 0600, replaced atomically (kit's fsx): a crash mid-write leaves the old
// file, never half a token.
type fileStore struct{ dir string }

func (s fileStore) load() (state, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return state{Users: map[string]connection{}}, nil
	}
	if err != nil {
		return state{}, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return state{}, err
	}
	if st.Users == nil {
		st.Users = map[string]connection{}
	}
	// An entry under a key no route could have written is dropped, not
	// trusted: every later path and lookup assumes a valid id.
	for id := range st.Users {
		if !validUserID(id) {
			delete(st.Users, id)
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

// metricsVersion changes when a stored metric's formula does: older entries
// are recomputed, never compared against new ones. A bump drops every
// user's whole file, so every stream is read again against the rate budget:
// a new key bumps entryRev instead.
const metricsVersion = 1

// entryRev is the key set a fresh entry carries. An older entry still serves
// what it has; a reader needing a newer key reads that activity's streams
// again (hr_zones). Absent keys alone cannot tell "stored before the key"
// from "no heart rate in this activity": that re-read every activity
// without heart rate on every call. 1: hr_max10, hr_20m, hr_20m_w.
const entryRev = 1

// maxStoredActivities bounds one user's metric file (a few hundred bytes
// each); the oldest go first.
const maxStoredActivities = 3000

// metricFile is one user's computed per-activity metrics: never raw streams.
type metricFile struct {
	V int `json:"v"`
	// Athlete is the Strava athlete the metrics were read for: a reconnect
	// to another athlete must not inherit them.
	Athlete    int64                    `json:"athlete,omitempty"`
	Activities map[string]storedMetrics `json:"activities"`
	// HRZones are the heart-rate zones per sport family (hrzones.go), as last
	// calculated from Activities: on every write of Activities and every
	// hr_zones call. Derived, so a metricsVersion bump may drop them.
	HRZones map[string]storedHRZones `json:"hr_zones,omitempty"`
}

// storedMetrics are the stream metrics of one activity that do not depend on
// FTP or zones; IF and TSS are derived from NP and MovingS with the current
// FTP whenever they are read, so an FTP change never serves stale values.
// Vals holds only known values (JSON has no NaN): np, avg_hr, hr_max10,
// hr_20m, hr_20m_w, ef, decoupling, avg_w, and mm_<seconds> for the
// mean-max curve.
type storedMetrics struct {
	V       int                `json:"v"`
	Rev     int                `json:"rev,omitempty"` // entryRev when stored
	Start   string             `json:"start"`         // start_date_local, for pruning
	MovingS int                `json:"moving_s"`
	Vals    map[string]float64 `json:"vals,omitempty"`
	// Sport and Name let the zones be calculated and cited from this file
	// alone. Entries from before them get both from hr_zones' list, free.
	Sport string `json:"sport,omitempty"`
	Name  string `json:"name,omitempty"`
}

func (s fileStore) metricsPath(user string) (string, error) {
	if !validUserID(user) {
		return "", fmt.Errorf("invalid user id")
	}
	return filepath.Join(s.dir, metricsDir, user+".json"), nil
}

func (s fileStore) loadMetrics(user string) (metricFile, error) {
	empty := metricFile{V: metricsVersion, Activities: map[string]storedMetrics{}}
	p, err := s.metricsPath(user)
	if err != nil {
		return empty, err
	}
	b, err := os.ReadFile(p) //nolint:gosec // p is built from a validated user id
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	return decodeMetrics(b, empty), nil
}

// decodeMetrics is the metric file in b, or empty when it is unreadable or
// from another formula: recomputed rather than failed on.
func decodeMetrics(b []byte, empty metricFile) metricFile {
	var m metricFile
	if json.Unmarshal(b, &m) != nil || m.V != metricsVersion {
		return empty
	}
	if m.Activities == nil {
		m.Activities = map[string]storedMetrics{}
	}
	return m
}

func (s fileStore) saveMetrics(user string, m metricFile) error {
	p, err := s.metricsPath(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	pruneMetrics(m.Activities, maxStoredActivities)
	m.V = metricsVersion
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(p, b, 0o600)
}

func (s fileStore) deleteMetrics(user string) error {
	p, err := s.metricsPath(user)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// pruneMetrics drops the oldest entries past max.
func pruneMetrics(acts map[string]storedMetrics, max int) {
	for len(acts) > max {
		oldest := ""
		for id, m := range acts {
			if oldest == "" || m.Start < acts[oldest].Start {
				oldest = id
			}
		}
		delete(acts, oldest)
	}
}
