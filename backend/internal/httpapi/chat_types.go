package httpapi

import "github.com/trick77/spark/internal/chat"

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (r updateProjectRequest) chatInput() chat.UpdateProjectInput {
	return chat.UpdateProjectInput{
		Name:        r.Name,
		Description: r.Description,
	}
}

type createThreadRequest struct {
	ProjectID *string `json:"projectId"`
	Title     string  `json:"title"`
}

type updateThreadRequest struct {
	Title *string `json:"title"`
}

type getThreadResponse struct {
	Thread   chat.Thread    `json:"thread"`
	Messages []chat.Message `json:"messages"`
}

type streamMessageRequest struct {
	Content string `json:"content"`
}

type streamDeltaResponse struct {
	Content string `json:"content"`
}

type toolCallResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolResultResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type mcpStatusResponse struct {
	Active     int `json:"active"`
	Configured int `json:"configured"`
}

type artifactResponse struct {
	ID              string  `json:"id"`
	DisplayFilename string  `json:"displayFilename"`
	MIMEType        string  `json:"mimeType"`
	SizeBytes       int64   `json:"sizeBytes"`
	ProjectID       *string `json:"projectId,omitempty"`
	DownloadURL     string  `json:"downloadUrl"`
	Model           string  `json:"model,omitempty"`
	Provider        string  `json:"provider,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationMs      int64   `json:"durationMs,omitempty"`
}
