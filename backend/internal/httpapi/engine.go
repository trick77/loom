package httpapi

import (
	"context"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
)

// Engine runs chat turns: it holds what a turn reads and writes, and nothing
// about HTTP. One Engine serves every turn; per-turn state lives in a Run.
type Engine struct {
	thread               ThreadStore
	usage                UsageStore
	artifacts            ArtifactStore
	documents            DocumentService
	llm                  ChatClient
	mcp                  ToolService
	docTools             []docgen.Generator
	imageTools           []imagegen.Tool
	sandbox              SandboxRunner
	imageDefaultModel    string
	imageTypographyModel string
	usersDir             string
	// knowledgeInlineTokenBudget bounds the full-document knowledge injected
	// per turn (0 disables it, falling back to pure RAG retrieval).
	knowledgeInlineTokenBudget int
	// projectSummaryTokenBudget bounds the cross-thread digest returned by the
	// read_project_threads tool.
	projectSummaryTokenBudget int
	memory                    Memory
}

// Memory is the user and project memory a turn reads into its prompt and
// refreshes after it. It lives outside the engine, which only calls it.
type Memory interface {
	// UserContext returns the user's memory block for the system prompt, or ""
	// when there is none.
	UserContext(ctx context.Context, userID string) string
	// ProjectContext returns the thread's project memory block for the system
	// prompt, or "" when the thread is not in a project or has none.
	ProjectContext(ctx context.Context, userID string, thread chat.Thread) string
	// RefreshProjectDescription refreshes a project's description in the
	// background after a turn titled one of its threads. It returns at once.
	RefreshProjectDescription(ctx context.Context, user auth.User, projectID string)
}

// recordUsage runs a best-effort usage-counter update against the engine's
// usage store; see recordUsageIn.
func (s *Engine) recordUsage(counter string, fn func() error) {
	recordUsageIn(s.usage, counter, fn)
}
