package artifact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/sqlutil"
)

// Store manages artifact metadata in the database.
type Store struct {
	db *sql.DB
}

// CreateInput holds the parameters for inserting a new artifact record.
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

// NewStore creates a new artifact store backed by the given database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create inserts a new artifact record and returns the created artifact with its assigned ID.
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

// DetachFromThread clears thread_id on the given artifacts, scoped to the user.
// It is used right before a thread is deleted, for artifacts whose thread id is
// only provenance and that still back a live document: with the column NULL the
// composite FK to threads no longer matches, so the thread's ON DELETE CASCADE
// leaves the row — and its documents.artifact_id link — intact.
func (s *Store) DetachFromThread(ctx context.Context, userID string, artifactIDs []string) error {
	if len(artifactIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("detach artifacts from thread: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range artifactIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE artifacts SET thread_id = NULL WHERE user_id = ? AND id = ?`,
			userID, id); err != nil {
			return fmt.Errorf("detach artifact %s from thread: %w", id, err)
		}
	}
	return tx.Commit()
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
	placeholders := sqlutil.Placeholders(len(ids))
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
	defer func() { _ = rows.Close() }()
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

// Get retrieves an artifact by id, scoped to the user, excluding soft-deleted artifacts.
// The returned bool indicates whether the artifact was found.
func (s *Store) Get(ctx context.Context, userID, artifactID string) (Artifact, bool, error) {
	out, err := scanArtifact(s.db.QueryRowContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
FROM artifacts
WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, artifactID))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, fmt.Errorf("get artifact: %w", err)
	}
	return out, true, nil
}

// List retrieves a paginated list of artifacts filtered and sorted according to
// the given options, each with its thread's title. Search matches the filename
// or that title.
func (s *Store) List(ctx context.Context, userID string, opts ListOptions) ([]ListItem, error) {
	limit := EffectiveArtifactLimit(opts.Limit)

	filters := []string{"a.user_id = ?", "a.deleted_at IS NULL"}
	args := []any{userID}
	if search := strings.TrimSpace(opts.Search); search != "" {
		pattern := "%" + sqlutil.EscapeLike(search) + "%"
		filters = append(filters, `(a.display_filename LIKE ? ESCAPE '\' OR t.title LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if filter := typeFilter(opts.Type); filter != "" {
		filters = append(filters, filter)
	}
	if !opts.Since.IsZero() {
		filters = append(filters, "a.created_at >= ?")
		args = append(args, sqliteTime(opts.Since))
	}
	if !opts.Until.IsZero() {
		filters = append(filters, "a.created_at < ?")
		args = append(args, sqliteTime(opts.Until))
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

	// The join is per row on threads' UNIQUE(user_id, id); a.user_id = ? keeps
	// it to the caller's own threads. Plan pinned by store's index_plan_test.
	query := fmt.Sprintf( //nolint:gosec // only fixed SQL structure is interpolated (a closed switch over typed constants); every value is a bound ? parameter
		`
SELECT a.id, a.user_id, a.thread_id, a.project_id, a.display_filename, a.volume_relpath, a.mime_type, a.size_bytes, a.source, a.created_at, a.thumbnail_relpath, COALESCE(t.title, '')
FROM artifacts a
LEFT JOIN threads t ON t.user_id = a.user_id AND t.id = a.thread_id
WHERE %s
ORDER BY %s
LIMIT ?`, strings.Join(filters, " AND "), listOrderBy(opts.Sort, opts.Order))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var items []ListItem
	for rows.Next() {
		var title string
		item, _, err := scanArtifactRow(rowScannerFunc(func(dest ...any) error {
			return rows.Scan(append(dest, &title)...)
		}), false)
		if err != nil {
			return nil, err
		}
		items = append(items, ListItem{Artifact: item, ThreadTitle: title})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan artifacts: %w", err)
	}
	return items, nil
}

// Summary counts the user's live artifacts per month in opts.Location, newest
// first, and those created before opts.Since, for the library's period picker.
func (s *Store) Summary(ctx context.Context, userID string, opts SummaryOptions) (Summary, error) {
	where := "a.user_id = ? AND a.deleted_at IS NULL"
	if filter := typeFilter(opts.Type); filter != "" {
		where += " AND " + filter
	}
	months, err := s.countMonths(ctx, where, userID, opts.Location)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{Months: months}
	if opts.Since.IsZero() {
		return out, nil
	}
	if err := s.db.QueryRowContext(ctx, //nolint:gosec // only fixed SQL from typeFilter is interpolated; every value is a bound ? parameter
		`SELECT COUNT(*) FROM artifacts a WHERE `+where+` AND a.created_at < ?`,
		userID, sqliteTime(opts.Since)).Scan(&out.OlderCount); err != nil {
		return Summary{}, fmt.Errorf("count older artifacts: %w", err)
	}
	return out, nil
}

// countMonths buckets the matching artifacts by month in loc (nil is UTC),
// newest first. In Go, not strftime: SQLite knows no IANA zones. Walking
// created_at newest first keeps each month's rows together, so a month ends
// where the key changes.
func (s *Store) countMonths(ctx context.Context, where, userID string, loc *time.Location) ([]MonthCount, error) {
	if loc == nil {
		loc = time.UTC
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.created_at FROM artifacts a WHERE `+where+` ORDER BY a.created_at DESC`, userID) //nolint:gosec // only fixed SQL from typeFilter is interpolated; the user id is a bound ? parameter
	if err != nil {
		return nil, fmt.Errorf("summarize artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	months := []MonthCount{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan artifact month: %w", err)
		}
		created, err := sqlutil.ParseTime(raw)
		if err != nil {
			return nil, fmt.Errorf("parse artifact created_at: %w", err)
		}
		month := created.In(loc).Format("2006-01")
		if n := len(months); n > 0 && months[n-1].Month == month {
			months[n-1].Count++
			continue
		}
		months = append(months, MonthCount{Month: month, Count: 1})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan artifact months: %w", err)
	}
	return months, nil
}

// typeFilter is the SQL condition for a library type filter on the artifacts
// alias "a", empty for all.
func typeFilter(t ListType) string {
	switch t {
	case ListTypeImages:
		return `a.mime_type LIKE 'image/%'`
	case ListTypeFiles:
		return `a.mime_type NOT LIKE 'image/%'`
	}
	return ""
}

// rowScannerFunc adapts a function to rowScanner, so List can scan its extra
// thread title after the shared artifact columns.
type rowScannerFunc func(dest ...any) error

func (f rowScannerFunc) Scan(dest ...any) error { return f(dest...) }

// ListForThread retrieves artifacts in a thread, scoped to the user.
func (s *Store) ListForThread(ctx context.Context, userID, threadID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
FROM artifacts
WHERE user_id = ? AND thread_id = ? AND deleted_at IS NULL
ORDER BY created_at ASC, rowid ASC`, userID, threadID)
	if err != nil {
		return nil, fmt.Errorf("list thread artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanArtifacts(rows)
}

func listOrderBy(sort SortBy, order SortOrder) string {
	direction := "DESC"
	if order == SortAsc {
		direction = "ASC"
	}
	// Alias "a" as in artifactKeysetClause; the two must name the same tuple.
	switch sort {
	case SortByName:
		return "a.display_filename COLLATE NOCASE " + direction + ", a.id " + direction
	case SortBySize:
		return "a.size_bytes " + direction + ", a.id " + direction
	default:
		return "a.created_at " + direction + ", a.id " + direction
	}
}

// ListForProject retrieves the artifacts a project's delete takes along,
// scoped to the user: the project's own and those of its threads, whose
// project_id may be none (a composer upload) or an earlier project's (made
// before the thread moved in). The threads cascade with the project, and
// their artifacts with them.
func (s *Store) ListForProject(ctx context.Context, userID, projectID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, thumbnail_relpath
FROM artifacts
WHERE user_id = ? AND deleted_at IS NULL AND (
	project_id = ?
	OR thread_id IN (SELECT t.id FROM threads t WHERE t.user_id = ? AND t.project_id = ?))
ORDER BY created_at ASC, rowid ASC`, userID, projectID, userID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
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

func scanArtifact(scanner rowScanner) (Artifact, error) {
	out, _, err := scanArtifactRow(scanner, false)
	return out, err
}

// scanArtifactWithDeleted scans a row that also selects the deleted_at column,
// returning the artifact plus the raw nullable deleted_at so the caller can set
// the Deleted flag. Used by GetMany, which (unlike the other queries) must surface
// soft-deleted rows.
func scanArtifactWithDeleted(scanner rowScanner) (Artifact, sql.NullString, error) {
	return scanArtifactRow(scanner, true)
}

// rowScanner is what *sql.Row and *sql.Rows share.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanArtifactRow scans the artifact columns in their SELECT order, followed by
// deleted_at when withDeleted is set, and derives the URL fields.
func scanArtifactRow(scanner rowScanner, withDeleted bool) (Artifact, sql.NullString, error) {
	var out Artifact
	var createdAt string
	var threadID, projectID, thumbnailRelPath, deletedAt sql.NullString
	dest := []any{
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
	}
	if withDeleted {
		dest = append(dest, &deletedAt)
	}
	if err := scanner.Scan(dest...); err != nil {
		return Artifact{}, sql.NullString{}, fmt.Errorf("scan artifact: %w", err)
	}
	out.ThreadID = threadID.String
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	out.ThumbnailRelPath = thumbnailRelPath.String
	parsed, err := sqlutil.ParseTime(createdAt)
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
		a.ThumbnailURL = "/api/artifacts/" + a.ID + ThumbnailURLSuffix
	}
}

// ThumbnailURLSuffix ends an artifact's thumbnail URL. The version query only
// busts the browser cache (see ThumbnailVersion); the endpoint ignores it, so
// URLs saved in messages and shares before it keep working.
const ThumbnailURLSuffix = "/thumbnail?v=" + ThumbnailVersion
