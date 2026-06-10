package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/chat"
)

type Store struct {
	db *sql.DB
}

type CreateInput struct {
	UserID          string
	ThreadID        string
	ProjectID       *string
	DisplayFilename string
	VolumeRelPath   string
	MIMEType        string
	SizeBytes       int64
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in CreateInput) (Artifact, error) {
	id := chat.NewIDForInternalUse()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.UserID, in.ThreadID, in.ProjectID, in.DisplayFilename, in.VolumeRelPath, in.MIMEType, in.SizeBytes,
	)
	if err != nil {
		return Artifact{}, fmt.Errorf("insert artifact: %w", err)
	}
	artifact, ok, err := s.Get(ctx, in.UserID, id)
	if err != nil {
		return Artifact{}, err
	}
	if !ok {
		return Artifact{}, fmt.Errorf("inserted artifact not found")
	}
	return artifact, nil
}

func (s *Store) Get(ctx context.Context, userID, artifactID string) (Artifact, bool, error) {
	var out Artifact
	var createdAt string
	var projectID sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts
WHERE user_id = ? AND id = ?`, userID, artifactID).Scan(
		&out.ID,
		&out.UserID,
		&out.ThreadID,
		&projectID,
		&out.DisplayFilename,
		&out.VolumeRelPath,
		&out.MIMEType,
		&out.SizeBytes,
		&out.Source,
		&createdAt,
	)
	if err == sql.ErrNoRows {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, fmt.Errorf("get artifact: %w", err)
	}
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	parsed, err := parseSQLiteTime(createdAt)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	out.DownloadURL = "/api/artifacts/" + out.ID + "/download"
	return out, true, nil
}

func (s *Store) List(ctx context.Context, userID string, opts ListOptions) ([]Artifact, error) {
	limit := EffectiveArtifactLimit(opts.Limit)

	filters := []string{"user_id = ?"}
	args := []any{userID}
	if search := strings.TrimSpace(opts.Search); search != "" {
		filters = append(filters, `display_filename LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(search)+"%")
	}
	switch opts.Type {
	case ListTypeImages:
		filters = append(filters, `mime_type LIKE 'image/%'`)
	case ListTypeFiles:
		filters = append(filters, `mime_type NOT LIKE 'image/%'`)
	}
	if opts.Cursor != "" {
		clause, boundValue, err := artifactKeysetClause(opts.Sort, opts.Order, opts.Cursor)
		if err != nil {
			return nil, err
		}
		// Keyset bound; the comparison expression (incl. COLLATE NOCASE for name)
		// must match listOrderBy so the page boundary aligns with the sort.
		filters = append(filters, clause.expr)
		args = append(args, boundValue, clause.id)
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts
WHERE %s
ORDER BY %s
LIMIT ?`, strings.Join(filters, " AND "), listOrderBy(opts.Sort, opts.Order))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func (s *Store) ListForThread(ctx context.Context, userID, threadID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts
WHERE user_id = ? AND thread_id = ?
ORDER BY created_at ASC`, userID, threadID)
	if err != nil {
		return nil, fmt.Errorf("list thread artifacts: %w", err)
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func listOrderBy(sort SortBy, order SortOrder) string {
	direction := "DESC"
	if order == SortAsc {
		direction = "ASC"
	}
	switch sort {
	case SortByName:
		return "display_filename COLLATE NOCASE " + direction + ", id " + direction
	case SortBySize:
		return "size_bytes " + direction + ", id " + direction
	default:
		return "created_at " + direction + ", id " + direction
	}
}

func escapeLike(term string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(term)
}

func (s *Store) ListForProject(ctx context.Context, userID, projectID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts
WHERE user_id = ? AND project_id = ?
ORDER BY created_at ASC`, userID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project artifacts: %w", err)
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func scanArtifacts(rows *sql.Rows) ([]Artifact, error) {
	var artifacts []Artifact
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan artifacts: %w", err)
	}
	return artifacts, nil
}

func scanArtifact(scanner interface {
	Scan(dest ...any) error
}) (Artifact, error) {
	var out Artifact
	var createdAt string
	var projectID sql.NullString
	if err := scanner.Scan(
		&out.ID,
		&out.UserID,
		&out.ThreadID,
		&projectID,
		&out.DisplayFilename,
		&out.VolumeRelPath,
		&out.MIMEType,
		&out.SizeBytes,
		&out.Source,
		&createdAt,
	); err != nil {
		return Artifact{}, fmt.Errorf("scan artifact: %w", err)
	}
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	parsed, err := parseSQLiteTime(createdAt)
	if err != nil {
		return Artifact{}, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	out.DownloadURL = "/api/artifacts/" + out.ID + "/download"
	return out, nil
}

func parseSQLiteTime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}
