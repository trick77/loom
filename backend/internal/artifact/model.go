package artifact

import "time"

// MaxDisplayFilenameLength is the character limit for artifact display filenames.
const (
	MaxDisplayFilenameLength = 180
	MaxArtifactSizeBytes     = 25 << 20
)

// Artifact represents a file uploaded or generated in the context of a chat thread or project.
type Artifact struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	ThreadID        string    `json:"threadId"`
	ProjectID       *string   `json:"projectId,omitempty"`
	DisplayFilename string    `json:"displayFilename"`
	VolumeRelPath   string    `json:"-"`
	MIMEType        string    `json:"mimeType"`
	SizeBytes       int64     `json:"sizeBytes"`
	Source          string    `json:"source"`
	CreatedAt       time.Time `json:"createdAt"`
	DownloadURL     string    `json:"downloadUrl"`
	// ThumbnailRelPath is the volume-relative path of the sidecar JPEG thumbnail,
	// empty when none has been generated (non-raster artifact, or not yet
	// backfilled). Internal only — the client never sees the path.
	ThumbnailRelPath string `json:"-"`
	// ThumbnailURL points at the thumbnail endpoint; set for raster image artifacts
	// (the endpoint lazily generates on first hit) and empty otherwise, so the UI
	// falls back to DownloadURL for SVGs and the typed icon for non-images.
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
	// Deleted is true when the artifact has been soft-deleted: its bytes are gone
	// from disk but the row is kept so chat messages can render a tombstone. The
	// Artifacts library filters these out; only GetMany surfaces them.
	Deleted bool `json:"deleted,omitempty"`
}

// ListType categorizes artifacts by content type for filtering.
type ListType string

const (
	// ListTypeAll is the default filter showing all artifacts.
	ListTypeAll ListType = "all"
	// ListTypeImages filters to show only image artifacts.
	ListTypeImages ListType = "images"
	// ListTypeFiles filters to show only non-image artifacts.
	ListTypeFiles ListType = "files"
)

// SortBy specifies the field used to order artifacts in list results.
type SortBy string

const (
	// SortByModified sorts by creation time, newest first.
	SortByModified SortBy = "modified"
	// SortByName sorts by display filename alphabetically.
	SortByName SortBy = "name"
	// SortBySize sorts by file size, largest first.
	SortBySize SortBy = "size"
)

// SortOrder specifies the direction for sorting artifact lists.
type SortOrder string

const (
	// SortAsc sorts in ascending order.
	SortAsc SortOrder = "asc"
	// SortDesc sorts in descending order.
	SortDesc SortOrder = "desc"
)

// ListOptions specifies filtering and pagination parameters for artifact lists.
type ListOptions struct {
	Search string
	Type   ListType
	Sort   SortBy
	Order  SortOrder
	Limit  int
	// Cursor is an opaque keyset position from a previous page; empty for the
	// first page.
	Cursor string
}

// OutputRequest specifies parameters for resolving an artifact's storage path.
type OutputRequest struct {
	UsersDir        string
	UserID          string
	ThreadID        string
	ProjectID       *string
	DisplayFilename string
	Extension       string
}

// OutputPath contains the paths and metadata for an artifact's storage location.
type OutputPath struct {
	AbsPath         string
	VolumeRelPath   string
	DisplayFilename string
	MIMEType        string
}

// MIMEType returns the MIME type for the given file extension.
func MIMEType(extension string) string {
	switch normalizeExtension(extension) {
	case "pdf":
		return "application/pdf"
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "csv":
		return "text/csv; charset=utf-8"
	case "html":
		return "text/html; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "xml":
		return "application/xml; charset=utf-8"
	case "md":
		return "text/markdown; charset=utf-8"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
