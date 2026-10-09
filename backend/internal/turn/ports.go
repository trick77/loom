package turn

import (
	"context"
	"encoding/json"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/sandbox"
	"github.com/trick77/loom/internal/usage"
)

// ThreadStore is the thread persistence dependency used by the HTTP handlers;
// chat.Store satisfies it.
type ThreadStore interface {
	// Projects.
	CreateProject(context.Context, string, chat.CreateProjectInput) (chat.Project, error)
	GetProject(context.Context, string, string) (chat.Project, bool, error)
	ListProjects(context.Context, string, bool) ([]chat.Project, error)
	UpdateProject(context.Context, string, string, chat.UpdateProjectInput) (chat.Project, bool, error)
	SetAutoProjectDescription(context.Context, string, string, string, int) (chat.Project, bool, error)
	SetProjectStarred(context.Context, string, string, bool) (chat.Project, bool, error)
	SetProjectArchived(context.Context, string, string, bool) (bool, error)
	DeleteProject(context.Context, string, string) (bool, error)
	ListProjectThreadTitles(context.Context, string, string) ([]string, error)

	// Threads.
	CreateThread(context.Context, string, chat.CreateThreadInput) (chat.Thread, error)
	GetThread(context.Context, string, string) (chat.Thread, bool, error)
	ListThreads(context.Context, string, chat.ListThreadsOptions) ([]chat.Thread, error)
	ListThreadIDs(context.Context, string, chat.ListThreadsOptions) ([]string, error)
	UpdateThread(context.Context, string, string, chat.UpdateThreadInput) (chat.Thread, bool, error)
	SetThreadStarred(context.Context, string, string, bool) (chat.Thread, bool, error)
	SetThreadImageModelIfEmpty(context.Context, string, string, string) (chat.Thread, bool, error)
	SetThreadTitleIfUnchanged(context.Context, string, string, string, string) (chat.Thread, bool, error)
	SetThreadArchived(context.Context, string, string, bool) (bool, error)
	DeleteThread(context.Context, string, string) (bool, error)

	// Messages.
	AddMessageWithAttachments(context.Context, string, string, chat.Role, string, json.RawMessage, json.RawMessage) (chat.Message, error)
	AddMessageWithCitations(context.Context, string, string, chat.Role, string, chat.MessageTokenUsage, json.RawMessage, json.RawMessage, json.RawMessage, json.RawMessage) (chat.Message, error)
	AddMessageCost(context.Context, string, string, int64) (bool, error)
	ListMessages(context.Context, string, string) ([]chat.Message, bool, error)
	ListRecentMessages(context.Context, string, string, int) ([]chat.Message, error)
	ListRecentMessagesForThreads(context.Context, string, []string, int) (map[string][]chat.Message, error)

	// Search.
	SearchMessages(context.Context, string, string, *string, string, int) ([]chat.MessageSearchHit, error)
	SearchThreadsByContent(context.Context, string, string, *string, int) ([]chat.ThreadContentHit, error)

	// Project memory.
	GetProjectMemory(context.Context, string, string) (chat.ProjectMemory, bool, error)
	UpsertProjectMemory(context.Context, string, string, string, int) (chat.ProjectMemory, error)
	CountProjectMessages(context.Context, string, string) (int, error)
	ListProjectMessages(context.Context, string, string, int) ([]chat.Message, error)

	// User memory.
	GetUserMemory(context.Context, string) (chat.UserMemory, bool, error)
	UpsertUserMemory(context.Context, string, string, int) (chat.UserMemory, error)
	CountUserMessages(context.Context, string) (int, error)
	ListUserMessages(context.Context, string, int) ([]chat.Message, error)

	// User directives.
	ListUserDirectives(context.Context, string) ([]chat.UserDirective, error)
	AddUserDirective(context.Context, string, string) (chat.UserDirective, error)
	RemoveUserDirective(context.Context, string, string) (bool, error)
	ReplaceUserDirective(context.Context, string, string, string) (chat.UserDirective, bool, error)

	// Shares.
	CreateShare(context.Context, string, chat.CreateShareInput) (chat.Share, error)
	GetShareByThreadID(context.Context, string, string) (chat.Share, bool, error)
	GetShareByShareID(context.Context, string) (chat.Share, bool, error)
	UpdateShareSnapshot(context.Context, string, string, chat.UpdateShareInput) (chat.Share, bool, error)
	SetShareEnabled(context.Context, string, string, bool) (bool, error)
	ListSharesForUser(context.Context, string) ([]chat.Share, error)
}

// UsageStore records per-user lifetime usage counters. All methods are
// best-effort from the caller's side; see httpapi's server.recordUsage.
type UsageStore interface {
	AddTokens(context.Context, string, usage.TokenDelta) error
	IncWebSearch(context.Context, string) error
	IncWebFetch(context.Context, string) error
	IncObscuraFetch(context.Context, string) error
	IncImageGen(context.Context, string) error
	IncCodeRun(context.Context, string) error
	IncThreadCreated(context.Context, string) error
	IncProjectCreated(context.Context, string) error
	Get(context.Context, string) (usage.Totals, error)
}

// ArtifactStore persists and looks up generated artifact metadata.
type ArtifactStore interface {
	Create(context.Context, artifact.CreateInput) (artifact.Artifact, error)
	Get(context.Context, string, string) (artifact.Artifact, bool, error)
	GetMany(context.Context, string, []string) (map[string]artifact.Artifact, error)
	Delete(context.Context, string, string) error
	Rename(context.Context, string, string, string) error
	SetThumbnailRelPath(context.Context, string, string, string) error
	DetachFromThread(context.Context, string, []string) error
	List(context.Context, string, artifact.ListOptions) ([]artifact.Artifact, error)
	ListForThread(context.Context, string, string) ([]artifact.Artifact, error)
	ListForProject(context.Context, string, string) ([]artifact.Artifact, error)
}

// ChatClient is the LLM dependency used by chat stream handlers.
type ChatClient interface {
	StreamChatWithTools(context.Context, []llm.Message, []llm.Tool, func(llm.StreamEvent) error) (llm.StreamResult, error)
	GenerateThreadTitle(context.Context, string, string, string) (string, error)
	ClassifyThread(context.Context, string) (string, error)
	ClassifyImageIntent(context.Context, string, bool, bool) (llm.ImageIntent, error)
	GenerateReasoningTitle(context.Context, string, string) (string, error)
	GenerateWorkingTitle(context.Context, string, string) (string, error)
	GenerateMemory(context.Context, string, string, string, string, string, string) (string, error)
	ApplyMemoryEdit(context.Context, string, string, string, string, string) (string, error)
	GenerateProjectDescription(context.Context, string, []string, string) (string, error)
}

// ToolService exposes configured MCP tools to chat handlers.
type ToolService interface {
	Tools() []llm.Tool
	ToolsFor(active map[string]bool) []llm.Tool
	CallTool(context.Context, string, map[string]any) (string, error)
	HasTool(string) bool
	ServerStatus(context.Context) []mcp.ServerStatus
}

// DocumentService is the RAG document dependency used by document handlers. It is
// nil when embeddings are not configured, which disables the feature (404).
type DocumentService interface {
	Upload(context.Context, documents.UploadInput) (rag.Document, artifact.Artifact, error)
	List(context.Context, string, *string) ([]rag.Document, error)
	Get(context.Context, string, string) (rag.Document, bool, error)
	FullText(context.Context, string, string) (string, error)
	Index(context.Context, string, string) error
	Delete(context.Context, string, string) error
	DeleteForArtifact(context.Context, string, string) (bool, error)
	DeleteThreadData(context.Context, string, string) error
	ArtifactIDsForThreadArtifactsInUse(context.Context, string, string) ([]string, error)
	DeleteProjectData(context.Context, string, string) error
	Retrieve(context.Context, string, *string, *string, string, int) ([]rag.RetrievedChunk, error)
	IndexedDocsInScope(context.Context, string, *string, *string) ([]rag.IndexedDoc, error)
	DocumentsInScope(context.Context, string, *string, *string, int) ([]rag.Document, error)
}

// SandboxRunner runs run_python jobs; *sandbox.Client implements it.
type SandboxRunner interface {
	Available() bool
	Timeout() time.Duration
	Run(context.Context, sandbox.Request) (sandbox.Result, error)
}
