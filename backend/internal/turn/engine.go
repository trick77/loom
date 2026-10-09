package turn

import (
	"context"
	"log/slog"

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

// Config is what New builds an Engine from. Nil stores and services turn the
// features that need them off, as the HTTP layer's Deps documents.
type Config struct {
	Thread    ThreadStore
	Usage     UsageStore
	Artifacts ArtifactStore
	Documents DocumentService
	LLM       ChatClient
	MCP       ToolService
	DocTools  []docgen.Generator
	// ImageTools generate images; ImageDefaultModel is the baseline image
	// model and ImageTypographyModel the one for typography work (empty turns
	// typography routing off).
	ImageTools           []imagegen.Tool
	ImageDefaultModel    string
	ImageTypographyModel string
	// Sandbox runs run_python; nil keeps the tool off.
	Sandbox  SandboxRunner
	UsersDir string
	// KnowledgeInlineTokenBudget bounds the full-document knowledge injected
	// per turn (0 disables it, falling back to pure RAG retrieval).
	KnowledgeInlineTokenBudget int
	// ProjectSummaryTokenBudget bounds the cross-thread digest returned by the
	// read_project_threads tool.
	ProjectSummaryTokenBudget int
	// Memory supplies the user and project context; required.
	Memory Memory
}

// New builds an Engine from c.
func New(c Config) *Engine {
	return &Engine{
		thread:                     c.Thread,
		usage:                      c.Usage,
		artifacts:                  c.Artifacts,
		documents:                  c.Documents,
		llm:                        c.LLM,
		mcp:                        c.MCP,
		docTools:                   c.DocTools,
		imageTools:                 c.ImageTools,
		sandbox:                    c.Sandbox,
		imageDefaultModel:          c.ImageDefaultModel,
		imageTypographyModel:       c.ImageTypographyModel,
		usersDir:                   c.UsersDir,
		knowledgeInlineTokenBudget: c.KnowledgeInlineTokenBudget,
		projectSummaryTokenBudget:  c.ProjectSummaryTokenBudget,
		memory:                     c.Memory,
	}
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
// usage store; see RecordUsage.
func (s *Engine) recordUsage(counter string, fn func() error) {
	RecordUsage(s.usage, counter, fn)
}

// RecordUsage runs a best-effort usage-counter update. A nil store (e.g. in
// tests) or any write error is logged and swallowed so counting never fails the
// underlying request. counter is a short label used only for logging.
func RecordUsage(store UsageStore, counter string, fn func() error) {
	if store == nil {
		return
	}
	if err := fn(); err != nil {
		slog.Warn("usage counter update failed", "counter", counter, "err", err)
	}
}
