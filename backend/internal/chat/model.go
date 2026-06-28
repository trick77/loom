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

const DefaultThreadTitle = "New thread"

const (
	MaxProjectNameLength        = 120
	MaxProjectDescriptionLength = 2000
	MaxThreadTitleLength        = 200
	MaxMessageContentLength     = 32000
)

// Project groups related chat threads for one user.
type Project struct {
	ID                         string     `json:"id"`
	UserID                     string     `json:"-"`
	Name                       string     `json:"name"`
	Description                string     `json:"description"`
	Starred                    bool       `json:"starred"`
	ArchivedAt                 *time.Time `json:"archivedAt"`
	AutoDescriptionGeneratedAt *time.Time `json:"-"`
	CreatedAt                  time.Time  `json:"createdAt"`
	UpdatedAt                  time.Time  `json:"updatedAt"`
	LastActivityAt             time.Time  `json:"lastActivityAt"`
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
const MaxProjectMemoryLength = 3000

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

// MaxUserMemoryLength hard-caps the stored user memory.
const MaxUserMemoryLength = 3000

// Thread is a single chat conversation.
type Thread struct {
	ID        string  `json:"id"`
	UserID    string  `json:"-"`
	ProjectID *string `json:"projectId"`
	Title     string  `json:"title"`
	// Category is the prompt-classifier label chosen on the first message (e.g.
	// "coding", "cooking_recipes"); empty until classified. Drives the
	// category-specific system-prompt block and the status-line pill.
	Category string `json:"category"`
	// ImageModel is the image-generation model locked on the first image generated
	// in this thread (e.g. "flux-2-klein-4b" or "flux-2-flex"); empty until the
	// first image. Once set it is reused for every later image in the thread so the
	// model never flip-flops mid-conversation.
	ImageModel    string     `json:"imageModel"`
	Starred       bool       `json:"starred"`
	ArchivedAt    *time.Time `json:"archivedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastMessageAt *time.Time `json:"lastMessageAt"`
	// Shared is true when an active public share link exists for this thread
	// (a shared_threads row with shared=1). Populated by ListThreads so chat
	// lists can badge shared threads; left false on single-thread fetches,
	// which expose share state separately via the Share summary.
	Shared bool `json:"shared"`
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
	// Attachments are the images and documents the user sent with this message,
	// persisted so a sent message's previews survive a reload. A JSON array of
	// MessageAttachment; "[]" for messages sent without attachments.
	Attachments   json.RawMessage `json:"attachments"`
	ActivityTrace json.RawMessage `json:"activityTrace"`
	// ContentBlocks is the ordered, interleaved timeline of this assistant
	// message — text prose, tool-activity trace runs, and artifacts — in the
	// chronological order they were produced. A JSON array of tagged blocks;
	// "[]" for messages without blocks (e.g. user/tool messages).
	ContentBlocks    json.RawMessage `json:"contentBlocks"`
	PromptTokens     *int            `json:"promptTokens,omitempty"`
	CompletionTokens *int            `json:"completionTokens,omitempty"`
	TotalTokens      *int            `json:"totalTokens,omitempty"`
	CachedTokens     *int            `json:"cachedTokens,omitempty"`
	ReasoningTokens  *int            `json:"reasoningTokens,omitempty"`
	// ContextTokens is the final answer call's model-reported total_tokens — the
	// real context size of that single generation, the correct basis for the
	// context-window percentage. Distinct from TotalTokens, which sums usage
	// across every model call in the turn. Nil for messages predating this field.
	ContextTokens   *int      `json:"contextTokens,omitempty"`
	DurationMs      *int      `json:"durationMs,omitempty"`
	Model           *string   `json:"model,omitempty"`
	ReasoningEffort *string   `json:"reasoningEffort,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

// MessageAttachment is one image or document a user sent with a message. It is
// the persisted, serialized shape stored in Message.Attachments. DownloadURL is
// computed at send time (deterministic from the artifact id) for images; it is
// empty for documents, which have no download endpoint yet.
type MessageAttachment struct {
	// Kind is "image" or "document".
	Kind        string `json:"kind"`
	ArtifactID  string `json:"artifactId,omitempty"`
	DocumentID  string `json:"documentId,omitempty"`
	Filename    string `json:"filename"`
	MIMEType    string `json:"mimeType"`
	SizeBytes   int64  `json:"sizeBytes"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	// ThumbnailURL points at the artifact's thumbnail endpoint for raster image
	// attachments (empty for SVGs and documents). Computed at send time and
	// persisted, so a reloaded sent image renders from the small thumbnail rather
	// than the full original. Older rows lack it and fall back to DownloadURL.
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}

const (
	AttachmentKindImage    = "image"
	AttachmentKindDocument = "document"
)

type MessageTokenUsage struct {
	PromptTokens     *int
	CompletionTokens *int
	TotalTokens      *int
	CachedTokens     *int
	ReasoningTokens  *int
	// ContextTokens is the final answer call's model-reported total_tokens (the
	// single generation's context size), persisted separately from the per-turn
	// accumulated TotalTokens so the UI can show true context-window occupancy.
	ContextTokens    *int
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
	Title *string
	// Category, when non-nil, sets the thread's prompt-classifier label. A nil
	// pointer leaves the stored category unchanged.
	Category  *string
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

// Share is a public, read-only snapshot of a thread. The row exists only once the
// thread has been shared; Shared toggles the public link on/off without losing the
// frozen snapshot or rotating ShareID. Snapshot is the sanitized JSON blob served
// to anonymous viewers; ArtifactIDs is the allowlist of generated-artifact ids the
// public artifact endpoints may serve for this share.
type Share struct {
	ID          string
	ShareID     string
	ThreadID    string
	UserID      string
	Shared      bool
	Title       string
	Snapshot    json.RawMessage
	ArtifactIDs []string
	SnapshotAt  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateShareInput struct {
	ShareID     string
	ThreadID    string
	Title       string
	Snapshot    json.RawMessage
	ArtifactIDs []string
}

type UpdateShareInput struct {
	Title       string
	Snapshot    json.RawMessage
	ArtifactIDs []string
}
