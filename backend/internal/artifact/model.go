package artifact

import "time"

const (
	MaxDisplayFilenameLength = 180
	MaxArtifactSizeBytes     = 25 << 20
)

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

type ListType string

const (
	ListTypeAll    ListType = "all"
	ListTypeImages ListType = "images"
	ListTypeFiles  ListType = "files"
)

type SortBy string

const (
	SortByModified SortBy = "modified"
	SortByName     SortBy = "name"
	SortBySize     SortBy = "size"
)

type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

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

type OutputRequest struct {
	UsersDir        string
	UserID          string
	ThreadID        string
	ProjectID       *string
	DisplayFilename string
	Extension       string
}

type OutputPath struct {
	AbsPath         string
	VolumeRelPath   string
	DisplayFilename string
	MIMEType        string
}

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
