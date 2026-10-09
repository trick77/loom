package artifact

// Response is the wire shape of an artifact: emitted by a chat turn when a
// tool creates one and returned by the artifact and thread read handlers.
type Response struct {
	ID              string  `json:"id"`
	DisplayFilename string  `json:"displayFilename"`
	MIMEType        string  `json:"mimeType"`
	SizeBytes       int64   `json:"sizeBytes"`
	ProjectID       *string `json:"projectId,omitempty"`
	DownloadURL     string  `json:"downloadUrl"`
	ThumbnailURL    string  `json:"thumbnailUrl,omitempty"`
	Model           string  `json:"model,omitempty"`
	Provider        string  `json:"provider,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationMs      int64   `json:"durationMs,omitempty"`
}

// Response is the wire shape of a stored artifact.
func (a Artifact) Response() Response {
	return Response{
		ID:              a.ID,
		DisplayFilename: a.DisplayFilename,
		MIMEType:        a.MIMEType,
		SizeBytes:       a.SizeBytes,
		ProjectID:       a.ProjectID,
		DownloadURL:     a.DownloadURL,
		ThumbnailURL:    a.ThumbnailURL,
	}
}
