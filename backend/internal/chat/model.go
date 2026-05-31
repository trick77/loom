package chat

import (
	"encoding/json"
	"time"
)

// Role identifies the source of a chat message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

const DefaultThreadTitle = "New chat"

const (
	MaxProjectNameLength        = 120
	MaxProjectDescriptionLength = 2000
	MaxThreadTitleLength        = 200
	MaxMessageContentLength     = 32000
)

// Project groups related chat threads for one user.
type Project struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ArchivedAt  *time.Time `json:"archivedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Thread is a single chat conversation.
type Thread struct {
	ID            string     `json:"id"`
	UserID        string     `json:"-"`
	ProjectID     *string    `json:"projectId"`
	Title         string     `json:"title"`
	Starred       bool       `json:"starred"`
	ArchivedAt    *time.Time `json:"archivedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastMessageAt *time.Time `json:"lastMessageAt"`
}

// Message is one item in a thread transcript.
type Message struct {
	ID        string          `json:"id"`
	ThreadID  string          `json:"threadId"`
	Role      Role            `json:"role"`
	Content   string          `json:"content"`
	ToolCalls json.RawMessage `json:"toolCalls"`
	Citations json.RawMessage `json:"citations"`
	CreatedAt time.Time       `json:"createdAt"`
}

type CreateProjectInput struct {
	Name        string
	Description string
}

type UpdateProjectInput struct {
	Name        *string
	Description *string
}

type CreateThreadInput struct {
	ProjectID *string
	Title     string
}

type UpdateThreadInput struct {
	Title *string
}

type ListThreadsOptions struct {
	ProjectID       *string
	ProjectlessOnly bool
	StarredOnly     bool
	Archived        bool
	Limit           int
}
