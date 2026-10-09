package turn

import (
	"context"
	"time"

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
	// reasoningTitleHold and reasoningTitleStartBytes tune the reasoning title
	// timing; zero means the default (see titleHold, titleStartBytes).
	reasoningTitleHold       time.Duration
	reasoningTitleStartBytes int
	// turnGateTimeout bounds the gate calls; zero means the default (see
	// gateTimeout).
	turnGateTimeout time.Duration
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
	// Memory supplies the user and project context; nil means none, as for a
	// user with no memory stored.
	Memory Memory
	// ReasoningTitleHold bounds how long the first answer word waits for its
	// round's reasoning title; ReasoningTitleStartBytes is how much reasoning
	// must have streamed before that title generates. Zero keeps the defaults;
	// tests shorten them.
	ReasoningTitleHold       time.Duration
	ReasoningTitleStartBytes int
	// TurnGateTimeout bounds each gate call (image intent, classification,
	// drift, thread title). Zero keeps the default; tests shorten it.
	TurnGateTimeout time.Duration
}

// New builds an Engine from c.
func New(c Config) *Engine {
	memory := c.Memory
	if memory == nil {
		memory = noMemory{}
	}
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
		memory:                     memory,
		reasoningTitleHold:         c.ReasoningTitleHold,
		reasoningTitleStartBytes:   c.ReasoningTitleStartBytes,
		turnGateTimeout:            c.TurnGateTimeout,
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

// noMemory is a Memory with no user or project context, as for a user with
// none stored. New uses it when Config.Memory is nil.
type noMemory struct{}

func (noMemory) UserContext(context.Context, string) string                   { return "" }
func (noMemory) ProjectContext(context.Context, string, chat.Thread) string   { return "" }
func (noMemory) RefreshProjectDescription(context.Context, auth.User, string) {}
