package turn

import (
	"context"
	"encoding/json"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/sandbox"
	"github.com/trick77/loom/internal/usage"
)

// The engine's ports hold only what a turn calls. The HTTP layer's stores
// and services are wider and satisfy them; chat.Store, usage.Store,
// artifact.Store and documents.Service are the production implementations.

// ThreadStore is the thread persistence a turn reads and writes.
type ThreadStore interface {
	// GetThread looks up a thread for the read_thread tool.
	GetThread(context.Context, string, string) (chat.Thread, bool, error)
	// ListThreads and ListRecentMessagesForThreads feed the
	// read_project_threads digest; ListRecentMessages feeds the per-thread
	// digest both thread tools render.
	ListThreads(context.Context, string, chat.ListThreadsOptions) ([]chat.Thread, error)
	ListRecentMessages(context.Context, string, string, int) ([]chat.Message, error)
	ListRecentMessagesForThreads(context.Context, string, []string, int) (map[string][]chat.Message, error)
	// UpdateThread stores the category a first turn classified;
	// SetThreadTitleIfUnchanged stores a generated title unless the user
	// renamed the thread meanwhile; SetThreadImageModelIfEmpty pins the
	// thread's image model on its first image.
	UpdateThread(context.Context, string, string, chat.UpdateThreadInput) (chat.Thread, bool, error)
	SetThreadTitleIfUnchanged(context.Context, string, string, string, string) (chat.Thread, bool, error)
	SetThreadImageModelIfEmpty(context.Context, string, string, string) (chat.Thread, bool, error)
	// AddMessageWithCitations persists the assistant answer; AddMessageCost
	// books the turn's spend not yet on a message (see CostSettler).
	AddMessageWithCitations(context.Context, string, string, chat.Role, string, chat.MessageTokenUsage, json.RawMessage, json.RawMessage, json.RawMessage, json.RawMessage) (chat.Message, error)
	AddMessageCost(context.Context, string, string, int64) (bool, error)
	// SearchMessages backs the conversation_search tool.
	SearchMessages(context.Context, string, string, *string, string, int) ([]chat.MessageSearchHit, error)
	// The user directive methods back the directive tools.
	ListUserDirectives(context.Context, string) ([]chat.UserDirective, error)
	AddUserDirective(context.Context, string, string) (chat.UserDirective, error)
	RemoveUserDirective(context.Context, string, string) (bool, error)
	ReplaceUserDirective(context.Context, string, string, string) (chat.UserDirective, bool, error)
}

// UsageStore records the lifetime usage counters a turn bumps: its tokens and
// the tool calls that count. All methods are best-effort from the caller's
// side; see usage.Record.
type UsageStore interface {
	AddTokens(context.Context, string, usage.TokenDelta) error
	IncWebSearch(context.Context, string) error
	IncWebFetch(context.Context, string) error
	IncObscuraFetch(context.Context, string) error
	IncImageGen(context.Context, string) error
	IncCodeRun(context.Context, string) error
}

// ArtifactStore records the artifacts a turn creates (generated files, images,
// code blocks) and looks up the ones it attaches or edits.
type ArtifactStore interface {
	Create(context.Context, artifact.CreateInput) (artifact.Artifact, error)
	Get(context.Context, string, string) (artifact.Artifact, bool, error)
	GetMany(context.Context, string, []string) (map[string]artifact.Artifact, error)
}

// ChatClient is the model a turn talks to: the answer stream and the short
// gates and titles around it.
type ChatClient interface {
	StreamChatWithTools(context.Context, []llm.Message, []llm.Tool, func(llm.StreamEvent) error) (llm.StreamResult, error)
	GenerateThreadTitle(context.Context, string, string, string) (string, error)
	ClassifyThread(context.Context, string) (string, error)
	ClassifyImageIntent(context.Context, string, bool, bool) (llm.ImageIntent, error)
	GenerateReasoningTitle(context.Context, string, string) (string, error)
	GenerateWorkingTitle(context.Context, string, string) (string, error)
}

// ToolService exposes the configured MCP tools a turn offers and calls.
type ToolService interface {
	Tools() []llm.Tool
	ToolsFor(active map[string]bool) []llm.Tool
	CallTool(context.Context, string, map[string]any) (string, error)
	HasTool(string) bool
}

// DocumentService is the RAG document store a turn reads its knowledge from:
// attached and in-scope documents inlined whole, retrieved chunks otherwise,
// and the files run_python gets. It is nil when embeddings are not
// configured, which turns knowledge off.
type DocumentService interface {
	Get(context.Context, string, string) (rag.Document, bool, error)
	FullText(context.Context, string, string) (string, error)
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
