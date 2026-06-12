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
	Starred     bool       `json:"starred"`
	ArchivedAt  *time.Time `json:"archivedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// ProjectMemory is a compact, auto-generated summary of a project's chats that
// is injected into every chat in the project so sibling chats share context.
// It is re-summarized (not appended) so it stays small and bounded.
type ProjectMemory struct {
	ProjectID string `json:"projectId"`
	Content   string `json:"content"`
	// SourceMessageCount records the total project message count at the last
	// refresh; it gates the background auto-refresh (see CountProjectMessages).
	SourceMessageCount int        `json:"-"`
	UpdatedAt          *time.Time `json:"updatedAt"`
}

// MaxProjectMemoryLength hard-caps the stored memory so a misbehaving model can
// never blow up the prompt; generation is asked to stay well under this.
const MaxProjectMemoryLength = 4000

// UserMemory is a compact, auto-generated set of durable facts about the user
// (employer, location, lasting preferences) that is injected into every chat the
// user has — project-bound or not — so the assistant stays personalized. Like
// ProjectMemory it is re-summarized (not appended) so it stays small and bounded.
type UserMemory struct {
	Content string `json:"content"`
	// SourceMessageCount records the total user message count at the last
	// refresh; it gates the background auto-refresh (see CountUserMessages).
	SourceMessageCount int        `json:"-"`
	UpdatedAt          *time.Time `json:"updatedAt"`
}

// MaxUserMemoryLength hard-caps the stored user memory. It is smaller than the
// project cap because user memory is a short, flat list of personal facts.
const MaxUserMemoryLength = 2000

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
	ID               string          `json:"id"`
	ThreadID         string          `json:"threadId"`
	Role             Role            `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoningContent,omitempty"`
	ToolCalls        json.RawMessage `json:"toolCalls"`
	Citations        json.RawMessage `json:"citations"`
	Artifacts        json.RawMessage `json:"artifacts"`
	ActivityTrace    json.RawMessage `json:"activityTrace"`
	PromptTokens     *int            `json:"promptTokens,omitempty"`
	CompletionTokens *int            `json:"completionTokens,omitempty"`
	TotalTokens      *int            `json:"totalTokens,omitempty"`
	CachedTokens     *int            `json:"cachedTokens,omitempty"`
	ReasoningTokens  *int            `json:"reasoningTokens,omitempty"`
	DurationMs       *int            `json:"durationMs,omitempty"`
	Model            *string         `json:"model,omitempty"`
	ReasoningEffort  *string         `json:"reasoningEffort,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
}

type MessageTokenUsage struct {
	PromptTokens     *int
	CompletionTokens *int
	TotalTokens      *int
	CachedTokens     *int
	ReasoningTokens  *int
	DurationMs       *int
	Model            *string
	ReasoningEffort  *string
	ReasoningContent string
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

type ProjectIDUpdate struct {
	Set   bool
	Value *string
}

type UpdateThreadInput struct {
	Title     *string
	ProjectID ProjectIDUpdate
}

type ListThreadsOptions struct {
	ProjectID       *string
	ProjectlessOnly bool
	StarredOnly     bool
	Archived        bool
	Search          string
	Limit           int
	// Cursor is an opaque keyset position from a previous page; empty for the
	// first page. Ignored by ListThreadIDs.
	Cursor string
}
