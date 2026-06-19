package httpapi

import (
	"encoding/json"
	"time"

	"github.com/trick77/loom/internal/chat"
)

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
	Title             *string
	ProjectID         *string
	ProjectIDProvided bool
}

func (r *updateThreadRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Title *string `json:"title"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Title = raw.Title
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	rawProjectID, ok := fields["projectId"]
	if !ok {
		return nil
	}
	r.ProjectIDProvided = true
	if string(rawProjectID) == "null" {
		r.ProjectID = nil
		return nil
	}
	var projectID string
	if err := json.Unmarshal(rawProjectID, &projectID); err != nil {
		return err
	}
	r.ProjectID = &projectID
	return nil
}

func (r updateThreadRequest) chatInput() chat.UpdateThreadInput {
	return chat.UpdateThreadInput{
		Title: r.Title,
		ProjectID: chat.ProjectIDUpdate{
			Set:   r.ProjectIDProvided,
			Value: r.ProjectID,
		},
	}
}

type listThreadsResponse struct {
	Items      []chat.Thread `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}

type bulkDeleteThreadsRequest struct {
	ThreadIDs []string `json:"threadIds"`
}

type bulkDeleteThreadsResponse struct {
	Deleted int `json:"deleted"`
}

type getThreadResponse struct {
	Thread   chat.Thread    `json:"thread"`
	Messages []chat.Message `json:"messages"`
}

type streamMessageRequest struct {
	Content               string   `json:"content"`
	DocumentAttachmentIDs []string `json:"documentAttachmentIds"`
	ImageAttachmentIDs    []string `json:"imageAttachmentIds"`
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
	Active     int               `json:"active"`
	Configured int               `json:"configured"`
	Servers    []mcpServerStatus `json:"servers,omitempty"`
}

type mcpServerStatus struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
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

type artifactListResponse struct {
	Items      []artifactListItemResponse `json:"items"`
	NextCursor *string                    `json:"nextCursor"`
}

type artifactListItemResponse struct {
	ID              string    `json:"id"`
	ThreadID        string    `json:"threadId"`
	ProjectID       *string   `json:"projectId,omitempty"`
	DisplayFilename string    `json:"displayFilename"`
	MIMEType        string    `json:"mimeType"`
	SizeBytes       int64     `json:"sizeBytes"`
	ModifiedAt      time.Time `json:"modifiedAt"`
	DownloadURL     string    `json:"downloadUrl"`
}
