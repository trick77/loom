package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/chat"
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
	// Source records how the artifact came to exist ("assistant_generated" by
	// default, "user_uploaded" for uploads). Empty falls back to the column default.
	Source string
	// ThumbnailRelPath is the volume-relative path of the eagerly-generated sidecar
	// thumbnail, empty when none was produced (non-raster, or generation failed).
	ThumbnailRelPath string
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in CreateInput) (Artifact, error) {
	id := chat.NewIDForInternalUse()
	source := in.Source
	if source == "" {
		source = "assistant_generated"
	}
	// A global (thread-less) upload stores NULL thread_id; the composite FK to
	// threads allows NULL but rejects a non-existent thread id.
	var threadID any
	if in.ThreadID != "" {
		threadID = in.ThreadID
	}
	// NULL when no thumbnail was generated, so the lazy backfill path can tell an
	// "untried" raster artifact apart from one that genuinely has no thumbnail.
	var thumbnailRelPath any
	if in.ThumbnailRelPath != "" {
		thumbnailRelPath = in.ThumbnailRelPath
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, thumbnail_relpath)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.UserID, threadID, in.ProjectID, in.DisplayFilename, in.VolumeRelPath, in.MIMEType, in.SizeBytes, source, thumbnailRelPath,
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

// Delete soft-deletes an artifact row scoped to the user: it stamps deleted_at so
// the row is hidden from the Artifacts library but kept for chat tombstones. It
// does not touch the file on disk; the caller handles volume cleanup. Re-deleting
// an already-deleted row is a no-op.
func (s *Store) Delete(ctx context.Context, userID, artifactID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE artifacts SET deleted_at = datetime('now') WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
		userID, artifactID)
	if err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

// Rename changes an artifact's display filename, scoped to the user. Only live
// (non-deleted) artifacts are renamable. The caller validates the new name.
func (s *Store) Rename(ctx context.Context, userID, artifactID, displayFilename string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE artifacts SET display_filename = ? WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
		displayFilename, userID, artifactID)
	if err != nil {
		return fmt.Errorf("rename artifact: %w", err)
	}
	return nil
}

// SetThumbnailRelPath records the volume-relative path of an artifact's sidecar
// thumbnail, scoped to the user. It is used by the lazy-backfill path on the
// thumbnail endpoint to persist a thumbnail generated on first view, so subsequent
// requests serve it directly. Only live (non-deleted) artifacts are updated.
func (s *Store) SetThumbnailRelPath(ctx context.Context, userID, artifactID, relPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE artifacts SET thumbnail_relpath = ? WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
		relPath, userID, artifactID)
	if err != nil {
		return fmt.Errorf("set artifact thumbnail: %w", err)
	}
	return nil
}

// GetMany loads artifacts by id for a user, INCLUDING soft-deleted ones, returned
// as a map keyed by id. It powers the read-time overlay that refreshes the
// artifact snapshots embedded in chat messages with the current display filename
// and deleted status. Ids the user does not own are simply absent from the map.
func (s *Store) GetMany(ctx context.Context, userID string, ids []string) (map[string]Artifact, error) {
	out := make(map[string]Artifact, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath, deleted_at
FROM artifacts
WHERE user_id = ? AND id IN (%s)`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("get many artifacts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, deletedAt, err := scanArtifactWithDeleted(rows)
		if err != nil {
			return nil, err
		}
		item.Deleted = deletedAt.Valid
		out[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan get many artifacts: %w", err)
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, userID, artifactID string) (Artifact, bool, error) {
	var out Artifact
	var createdAt string
	var threadID, projectID, thumbnailRelPath sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
FROM artifacts
WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, artifactID).Scan(
		&out.ID,
		&out.UserID,
		&threadID,
		&projectID,
		&out.DisplayFilename,
		&out.VolumeRelPath,
		&out.MIMEType,
		&out.SizeBytes,
		&out.Source,
		&createdAt,
		&thumbnailRelPath,
	)
	if err == sql.ErrNoRows {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, fmt.Errorf("get artifact: %w", err)
	}
	out.ThreadID = threadID.String
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	out.ThumbnailRelPath = thumbnailRelPath.String
	parsed, err := parseSQLiteTime(createdAt)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	setArtifactURLs(&out)
	return out, true, nil
}

func (s *Store) List(ctx context.Context, userID string, opts ListOptions) ([]Artifact, error) {
	limit := EffectiveArtifactLimit(opts.Limit)

	filters := []string{"user_id = ?", "deleted_at IS NULL"}
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
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
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
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
FROM artifacts
WHERE user_id = ? AND thread_id = ? AND deleted_at IS NULL
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
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
FROM artifacts
WHERE user_id = ? AND project_id = ? AND deleted_at IS NULL
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
	var threadID, projectID, thumbnailRelPath sql.NullString
	if err := scanner.Scan(
		&out.ID,
		&out.UserID,
		&threadID,
		&projectID,
		&out.DisplayFilename,
		&out.VolumeRelPath,
		&out.MIMEType,
		&out.SizeBytes,
		&out.Source,
		&createdAt,
		&thumbnailRelPath,
	); err != nil {
		return Artifact{}, fmt.Errorf("scan artifact: %w", err)
	}
	out.ThreadID = threadID.String
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	out.ThumbnailRelPath = thumbnailRelPath.String
	parsed, err := parseSQLiteTime(createdAt)
	if err != nil {
		return Artifact{}, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	setArtifactURLs(&out)
	return out, nil
}

// scanArtifactWithDeleted scans a row that also selects the deleted_at column,
// returning the artifact plus the raw nullable deleted_at so the caller can set
// the Deleted flag. Used by GetMany, which (unlike the other queries) must surface
// soft-deleted rows.
func scanArtifactWithDeleted(scanner interface {
	Scan(dest ...any) error
}) (Artifact, sql.NullString, error) {
	var out Artifact
	var createdAt string
	var threadID, projectID, thumbnailRelPath, deletedAt sql.NullString
	if err := scanner.Scan(
		&out.ID,
		&out.UserID,
		&threadID,
		&projectID,
		&out.DisplayFilename,
		&out.VolumeRelPath,
		&out.MIMEType,
		&out.SizeBytes,
		&out.Source,
		&createdAt,
		&thumbnailRelPath,
		&deletedAt,
	); err != nil {
		return Artifact{}, sql.NullString{}, fmt.Errorf("scan artifact: %w", err)
	}
	out.ThreadID = threadID.String
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	out.ThumbnailRelPath = thumbnailRelPath.String
	parsed, err := parseSQLiteTime(createdAt)
	if err != nil {
		return Artifact{}, sql.NullString{}, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	setArtifactURLs(&out)
	return out, deletedAt, nil
}

// setArtifactURLs fills the derived URL fields from the artifact's id and MIME
// type. The download URL is always present; the thumbnail URL is set only for
// raster image artifacts (the thumbnail endpoint lazily generates on first hit),
// so SVGs and non-images carry no thumbnail URL and the client falls back to the
// original. Call after the id and MIME type have been scanned.
func setArtifactURLs(a *Artifact) {
	a.DownloadURL = "/api/artifacts/" + a.ID + "/download"
	if IsThumbnailableMIME(a.MIMEType) {
		a.ThumbnailURL = "/api/artifacts/" + a.ID + "/thumbnail"
	}
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
