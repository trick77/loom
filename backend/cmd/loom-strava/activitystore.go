package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/trick77/ship/kit/fsx"
)

// The activity store keeps what Strava answered about each activity, per
// user, so a question asked twice (or after a restart) costs no Strava call:
// <data>/activities/<loom id>/index.json.gz holds every summary ever listed,
// <id>.<kind>.json.gz the raw answer of one activity's detail, streams or
// zones. Activities barely change after upload; the refresh argument of the
// tools reads one again when the user says it did.
const (
	activitiesDir = "activities"
	indexFile     = "index.json.gz"
	// indexVersion changes when the index layout does: an older one is read
	// again from Strava, the raw bodies stay.
	indexVersion = 1

	kindDetail  = "detail"
	kindStreams = "streams"
	kindZones   = "zones"

	// settleTime: a detail younger than this (end of the activity) is not
	// stored, since Strava may still be matching segments and best efforts.
	// Streams are final at upload and are stored at once.
	settleTime = time.Hour
	// headWindow is how far back each list sync reads again before the
	// newest stored activity: what changed or went in it is caught there.
	headWindow = 30 * 24 * time.Hour
	// maxHeadPages and maxBackfillPages bound the list reads of one sync.
	maxHeadPages     = 5
	maxBackfillPages = 25
	// refreshDebounce: a refresh of the same activity within this is
	// answered from the store.
	refreshDebounce = 10 * time.Minute
)

// maxStreamBytes bounds one user's stored streams (gzipped, ~200-400 KB a
// 3 h ride): the least recently read go first. Details and summaries are
// small and stay. Nothing stored expires, by design: activities do not
// change, and every reread costs the rate budget all users share. A
// variable for the tests.
var maxStreamBytes int64 = 1 << 30

// activityIndex is one user's list of activities, as far as it was read.
type activityIndex struct {
	V int `json:"v"`
	// Athlete is whose activities these are: another athlete's never count.
	Athlete int64 `json:"athlete,omitempty"`
	// Complete: the backfill reached the user's first activity.
	Complete bool `json:"complete,omitempty"`
	// Cursor is the start (unix) of the oldest activity read so far; the
	// backfill goes on before it.
	Cursor int64     `json:"cursor,omitempty"`
	Synced time.Time `json:"synced,omitzero"`
	// Activities by id.
	Activities map[string]summaryActivity `json:"activities"`
	// Streams says which stored streams exist, their size and last read,
	// for maxStreamBytes.
	Streams map[string]streamMeta `json:"streams,omitempty"`
	// Seen: when a head read brought an activity new to the index, kept
	// for settleTime (a late upload's detail is not final yet).
	Seen map[string]time.Time `json:"seen,omitempty"`
}

type streamMeta struct {
	Bytes  int64     `json:"bytes"`
	ReadAt time.Time `json:"read_at"`
}

func newIndex(athlete int64) *activityIndex {
	return &activityIndex{V: indexVersion, Athlete: athlete,
		Activities: map[string]summaryActivity{}, Streams: map[string]streamMeta{}, Seen: map[string]time.Time{}}
}

// newest is the start of the newest activity listed, zero without one.
func (x *activityIndex) newest() time.Time {
	var t time.Time
	for _, a := range x.Activities {
		if s := a.utcStart(); s.After(t) {
			t = s
		}
	}
	return t
}

// between is every listed activity starting after after and before before
// (unix seconds, 0 = open), newest first.
func (x *activityIndex) between(after, before int64) []summaryActivity {
	out := []summaryActivity{}
	for _, a := range x.Activities {
		s := a.utcStart().Unix()
		if (after > 0 && s <= after) || (before > 0 && s >= before) {
			continue
		}
		out = append(out, a)
	}
	slices.SortStableFunc(out, func(p, q summaryActivity) int {
		if c := q.utcStart().Compare(p.utcStart()); c != 0 {
			return c
		}
		return int(q.ID - p.ID)
	})
	return out
}

// streamBytes is the size of every stored stream.
func (x *activityIndex) streamBytes() int64 {
	var n int64
	for _, m := range x.Streams {
		n += m.Bytes
	}
	return n
}

// --- files ---

func (s fileStore) activityDir(user string) (string, error) {
	if !validUserID(user) {
		return "", fmt.Errorf("invalid user id")
	}
	return filepath.Join(s.dir, activitiesDir, user), nil
}

func (s fileStore) bodyPath(user string, id int64, kind string) (string, error) {
	d, err := s.activityDir(user)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, strconv.FormatInt(id, 10)+"."+kind+".json.gz"), nil
}

// readBody is a stored Strava answer; false when there is none or it is
// unreadable.
func (s fileStore) readBody(user string, id int64, kind string) ([]byte, bool) {
	p, err := s.bodyPath(user, id, kind)
	if err != nil {
		return nil, false
	}
	b, err := readGzip(p)
	if err != nil {
		// A corrupt body is missing: the next read replaces it. A file that
		// would not open (too many open files) stays.
		if errors.Is(err, errCorrupt) {
			_ = os.Remove(p)
		}
		return nil, false
	}
	return b, true
}

// writeBody stores a Strava answer, replacing the one before; it returns
// the bytes written.
func (s fileStore) writeBody(user string, id int64, kind string, body []byte) (int64, error) {
	p, err := s.bodyPath(user, id, kind)
	if err != nil {
		return 0, err
	}
	z, err := gzipBytes(body)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return 0, err
	}
	return int64(len(z)), fsx.WriteFileAtomic(p, z, 0o600)
}

func (s fileStore) removeBodies(user string, id int64, kinds ...string) {
	for _, k := range kinds {
		if p, err := s.bodyPath(user, id, k); err == nil {
			_ = os.Remove(p)
		}
	}
}

// loadIndex is the user's index, or nil when there is none, it is from
// another layout, or unreadable: the list is then read from Strava again.
func (s fileStore) loadIndex(user string) *activityIndex {
	d, err := s.activityDir(user)
	if err != nil {
		return nil
	}
	b, err := readGzip(filepath.Join(d, indexFile))
	if err != nil {
		return nil
	}
	var x activityIndex
	if json.Unmarshal(b, &x) != nil || x.V != indexVersion {
		return nil
	}
	if x.Activities == nil {
		x.Activities = map[string]summaryActivity{}
	}
	if x.Streams == nil {
		x.Streams = map[string]streamMeta{}
	}
	if x.Seen == nil {
		x.Seen = map[string]time.Time{}
	}
	return &x
}

func (s fileStore) saveIndex(user string, x *activityIndex) error {
	d, err := s.activityDir(user)
	if err != nil {
		return err
	}
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	z, err := gzipBytes(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(filepath.Join(d, indexFile), z, 0o600)
}

// deleteActivities removes everything stored for the user's activities.
func (s fileStore) deleteActivities(user string) error {
	d, err := s.activityDir(user)
	if err != nil {
		return err
	}
	return os.RemoveAll(d)
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// errCorrupt: a stored file opened but is not the gzip it should be.
var errCorrupt = errors.New("corrupt stored file")

func readGzip(p string) ([]byte, error) {
	f, err := os.Open(p) //nolint:gosec // p is built from a validated user id and a numeric activity id
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCorrupt, err)
	}
	b, err := io.ReadAll(io.LimitReader(zr, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCorrupt, err)
	}
	return b, nil
}
