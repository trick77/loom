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
	default:
		return "text/plain; charset=utf-8"
	}
}
