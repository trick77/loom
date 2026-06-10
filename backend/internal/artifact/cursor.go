package artifact

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
)

// sqliteTimeLayout matches the textual datetime format SQLite stores and orders
// by, so cursor bounds line up with the lexical ORDER BY on created_at.
//
// Invariant: created_at is written via datetime('now'), i.e. always UTC in
// exactly this layout. The modified-sort cursor compares this rendered bound
// lexically against the raw column text, so a row stored with a different
// format (fractional seconds, 'T'/'Z', offset) would silently shift the page
// boundary. The existing ORDER BY depends on the same invariant.
const sqliteTimeLayout = "2006-01-02 15:04:05"

const (
	defaultArtifactLimit = 1000
	maxArtifactLimit     = 1000
)

// artifactCursorPayload is the keyset position of the last returned artifact,
// mirroring the ORDER BY tuple (sort field, id).
type artifactCursorPayload struct {
	Value string `json:"v"`
	ID    string `json:"id"`
}

// EncodeArtifactCursor builds the opaque cursor pointing just past the given
// artifact for the active sort, so the next page resumes the same ORDER BY.
func EncodeArtifactCursor(a Artifact, sort SortBy) string {
	var value string
	switch sort {
	case SortByName:
		value = a.DisplayFilename
	case SortBySize:
		value = strconv.FormatInt(a.SizeBytes, 10)
	default:
		value = a.CreatedAt.UTC().Format(sqliteTimeLayout)
	}
	raw, _ := json.Marshal(artifactCursorPayload{Value: value, ID: a.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeArtifactCursor(encoded string) (artifactCursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return artifactCursorPayload{}, fmt.Errorf("decode cursor: %w", err)
	}
	var payload artifactCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return artifactCursorPayload{}, fmt.Errorf("parse cursor: %w", err)
	}
	if payload.ID == "" {
		return artifactCursorPayload{}, fmt.Errorf("invalid cursor")
	}
	return payload, nil
}

// artifactKeyset is a decoded cursor rendered into a SQL keyset clause plus the
// trailing id bound; the value bound is returned separately so size sorts can
// bind a numeric type.
type artifactKeyset struct {
	expr string
	id   string
}

func artifactKeysetClause(sort SortBy, order SortOrder, cursor string) (artifactKeyset, any, error) {
	payload, err := decodeArtifactCursor(cursor)
	if err != nil {
		return artifactKeyset{}, nil, err
	}
	cmp := "<"
	if order == SortAsc {
		cmp = ">"
	}
	var expr string
	var bound any = payload.Value
	switch sort {
	case SortByName:
		expr = "display_filename COLLATE NOCASE"
	case SortBySize:
		expr = "size_bytes"
		n, err := strconv.ParseInt(payload.Value, 10, 64)
		if err != nil {
			return artifactKeyset{}, nil, fmt.Errorf("invalid cursor: %w", err)
		}
		bound = n
	default:
		expr = "created_at"
	}
	return artifactKeyset{
		expr: fmt.Sprintf("(%s, id) %s (?, ?)", expr, cmp),
		id:   payload.ID,
	}, bound, nil
}

// EffectiveArtifactLimit applies the same default/cap as List, so callers can
// decide whether a full page was returned (and thus a next cursor exists).
func EffectiveArtifactLimit(limit int) int {
	if limit <= 0 {
		return defaultArtifactLimit
	}
	if limit > maxArtifactLimit {
		return maxArtifactLimit
	}
	return limit
}
