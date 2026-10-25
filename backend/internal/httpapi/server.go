// Package httpapi builds loom's HTTP handler: JSON/SSE API plus the embedded SPA.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/background"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/turn"
	"github.com/trick77/loom/internal/usage"
	"github.com/trick77/loom/web"
	kitauth "github.com/trick77/ship/kit/auth"
	"github.com/trick77/ship/kit/httpx"
)

// Deps are the dependencies needed to build the server.
type Deps struct {
	Static http.Handler // serves the embedded SPA; may be nil in tests
	// IndexHTML is the SPA shell the share page renders its card into; nil is
	// the embedded build's (web.IndexHTML).
	IndexHTML func() ([]byte, bool)
	// Model describes the configured chat model for /api/model; zero when
	// chat is off.
	Model llm.ModelInfo

	OIDC       OIDCService
	Auth       *auth.Middleware
	Sessions   SessionService
	Users      UserService
	Thread     ThreadStore
	Usage      UsageStore
	Artifacts  ArtifactStore
	Documents  DocumentService
	LLM        ChatClient
	MCP        ToolService
	DocTools   []docgen.Generator
	ImageTools []imagegen.Tool
	// Sandbox runs run_python; nil keeps the tool off.
	Sandbox SandboxRunner
	// ImageDefaultModel is the configured baseline image model (e.g.
	// "flux-2-klein-4b"); ImageGenTypographyModel (e.g. "flux-2-flex") is used instead
	// for the first image of a thread when it reads as typography/logo/text work.
	// Empty ImageGenTypographyModel (config: "off") disables typography routing.
	ImageDefaultModel       string
	ImageGenTypographyModel string
	UsersDir                string
	OIDCAdminGroup          string
	DevAuthClaims           auth.Claims
	PostLogoutRedirectURL   string
	// PublicURL is the externally reachable base URL (https://<PUBLIC_FQDN>), used
	// to build absolute share links. Empty without PUBLIC_FQDN → callers fall back
	// to a relative /share/<id> path.
	PublicURL string
	// KnowledgeInlineTokenBudget bounds the full-document knowledge injected per
	// turn (0 disables it, falling back to pure RAG retrieval).
	KnowledgeInlineTokenBudget int
	// ProjectSummaryTokenBudget bounds the cross-thread digest returned by the
	// read_project_threads tool.
	ProjectSummaryTokenBudget int
	// Background owns the goroutines that outlive a request (post-turn memory
	// refreshes). nil means a group nobody stops, which is what tests want.
	Background *background.Group
	// Lifetime ends when the server shuts down. A chat turn runs detached from
	// its client, so a dropped connection does not end it; this is what still
	// stops it on shutdown. nil means a lifetime that never ends.
	Lifetime context.Context
	// ReasoningTitleHold and ReasoningTitleStartBytes tune the reasoning title
	// timing (see turn.Config); zero keeps the defaults, tests shorten them.
	ReasoningTitleHold       time.Duration
	ReasoningTitleStartBytes int
	// Schedules stores scheduled tasks and notifications; ScheduleLLM reads
	// intervals and a run's findings. Both nil keep scheduled tasks off.
	Schedules   *schedule.Store
	ScheduleLLM ScheduleLLM
}

// ScheduleLLM is the model work scheduled tasks need beyond a chat turn;
// *llm.Client is one.
type ScheduleLLM interface {
	ParseSchedule(ctx context.Context, text string) (string, bool, error)
	ExtractFindings(ctx context.Context, task, answer string, knownKeys []string) ([]llm.Finding, error)
}

type server struct {
	model                 llm.ModelInfo
	background            *background.Group
	inflight              inflightKeys
	oidc                  OIDCService
	auth                  *auth.Middleware
	sessions              SessionService
	users                 UserService
	thread                ThreadStore
	usage                 UsageStore
	artifacts             ArtifactStore
	documents             DocumentService
	llm                   ChatClient
	mcp                   ToolService
	usersDir              string
	faviconCacheDir       string
	faviconClient         *http.Client             // nil → faviconDefaultClient (overridden in tests)
	faviconService        func(host string) string // nil → Google's favicon service (overridden in tests)
	oidcAdminGroup        string
	devAuthClaims         auth.Claims
	postLogoutRedirectURL string
	publicURL             string
	indexHTML             func() ([]byte, bool)
	activeStreams         activeStreamRegistry
	lifetime              context.Context
	// engine runs the chat turns the stream handlers start.
	engine *turn.Engine
	// schedules is nil while scheduled tasks are off; see scheduleRuns.
	schedules   *schedule.Store
	scheduleSvc *schedule.Service
	scheduleLLM ScheduleLLM
	runs        *scheduleRuns
	// timezones holds each user's browser timezone, by user id.
	timezones sync.Map
	// now is the clock scheduled runs read; nil is time.Now. runTimeout and
	// runSoftDeadline replace schedule.RunTimeout and RunSoftDeadline when set
	// (tests).
	now             func() time.Time
	runTimeout      time.Duration
	runSoftDeadline time.Duration
}

// The stores and services below are the HTTP layer's full dependencies. Each
// also satisfies the narrower port of the same name in package turn, which
// holds only what a chat turn calls; newServer hands them to the engine.

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
	AddMessageWithAttachments(context.Context, string, string, chat.Role, string, json.RawMessage, json.RawMessage, string) (chat.Message, error)
	AddMessageWithCitations(context.Context, string, string, chat.Role, string, chat.MessageTokenUsage, json.RawMessage, json.RawMessage, json.RawMessage, json.RawMessage) (chat.Message, error)
	AddMessageCost(context.Context, string, string, int64) (bool, error)
	ListMessages(context.Context, string, string) ([]chat.Message, bool, error)
	ListRecentMessages(context.Context, string, string, int) ([]chat.Message, error)
	ListRecentMessagesForThreads(context.Context, string, []string, int) (map[string][]chat.Message, error)

	// Queued messages: sent while a turn ran, waiting for it.
	EnqueueMessage(context.Context, string, string, string, json.RawMessage, string) (chat.QueuedMessage, error)
	ListQueued(context.Context, string, string) ([]chat.QueuedMessage, error)
	HasQueued(context.Context, string, string) (bool, error)
	DeleteQueued(context.Context, string, string, string) (bool, error)
	DrainQueued(context.Context, string, string) ([]chat.QueuedMessage, error)
	ConsumeQueued(context.Context, string, string) ([]chat.Message, error)
	ThreadsWithQueued(context.Context) ([]chat.QueuedThread, error)

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
// best-effort from the caller's side; see usage.Record.
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

// ChatClient is the LLM dependency used by chat stream handlers and the memory
// refreshes.
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
	turn.ToolService
	ServerStatus(context.Context) []mcp.ServerStatus
	// ServerAllowed hides a server limited to other accounts from /mcp.
	ServerAllowed(user mcp.Principal, server string) bool
}

// SandboxRunner runs run_python jobs; *sandbox.Client implements it. The
// handlers need nothing beyond the engine's port, so it is the same type.
type SandboxRunner = turn.SandboxRunner

// OIDCService is the auth handler dependency for OIDC redirects and
// callbacks; *kitauth.OIDC is one.
type OIDCService interface {
	Start(http.ResponseWriter, *http.Request)
	Callback(http.ResponseWriter, *http.Request) (kitauth.Claims, error)
}

// SessionService issues and clears the session cookie; *kitauth.Sessions is
// one.
type SessionService interface {
	Issue(http.ResponseWriter, kitauth.Claims) error
	Clear(http.ResponseWriter)
}

// UserService is the user dependency used by auth handlers.
type UserService interface {
	auth.UserLookup
	UpsertFromClaims(context.Context, auth.Claims, string) (auth.User, error)
	ListUsers(context.Context) ([]auth.User, error)
	UpdateResponseLanguage(ctx context.Context, id, language string) error
}

// newServer builds the server struct from its dependencies.
func newServer(d Deps) *server {
	bg := d.Background
	if bg == nil {
		bg = background.New(context.Background())
	}
	lifetime := d.Lifetime
	if lifetime == nil {
		lifetime = context.Background()
	}
	s := &server{
		background:            bg,
		lifetime:              lifetime,
		model:                 d.Model,
		oidc:                  d.OIDC,
		auth:                  d.Auth,
		sessions:              d.Sessions,
		users:                 d.Users,
		thread:                d.Thread,
		usage:                 d.Usage,
		artifacts:             d.Artifacts,
		documents:             d.Documents,
		llm:                   d.LLM,
		mcp:                   d.MCP,
		usersDir:              d.UsersDir,
		faviconCacheDir:       faviconCacheDirFor(d.UsersDir),
		oidcAdminGroup:        d.OIDCAdminGroup,
		devAuthClaims:         d.DevAuthClaims,
		postLogoutRedirectURL: d.PostLogoutRedirectURL,
		publicURL:             d.PublicURL,
		indexHTML:             d.IndexHTML,
	}
	if s.indexHTML == nil {
		s.indexHTML = web.IndexHTML
	}
	var scheduleTools turn.ScheduleTools
	if d.Schedules != nil && d.ScheduleLLM != nil {
		s.schedules, s.scheduleLLM = d.Schedules, d.ScheduleLLM
		s.scheduleSvc = &schedule.Service{
			Store:       d.Schedules,
			Interpreter: d.ScheduleLLM,
			Namer:       scheduleNamer{s: s},
			Now:         s.clock,
		}
		s.runs = newScheduleRuns()
		scheduleTools = scheduleToolRunner{s: s}
	}
	s.engine = turn.New(turn.Config{
		Schedules:                  scheduleTools,
		Thread:                     d.Thread,
		Usage:                      d.Usage,
		Artifacts:                  d.Artifacts,
		Documents:                  d.Documents,
		LLM:                        d.LLM,
		MCP:                        d.MCP,
		DocTools:                   d.DocTools,
		ImageTools:                 d.ImageTools,
		ImageDefaultModel:          d.ImageDefaultModel,
		ImageTypographyModel:       d.ImageGenTypographyModel,
		Sandbox:                    d.Sandbox,
		UsersDir:                   d.UsersDir,
		KnowledgeInlineTokenBudget: d.KnowledgeInlineTokenBudget,
		ProjectSummaryTokenBudget:  d.ProjectSummaryTokenBudget,
		Memory:                     engineMemory{s: s},
		ReasoningTitleHold:         d.ReasoningTitleHold,
		ReasoningTitleStartBytes:   d.ReasoningTitleStartBytes,
	})
	return s
}

// NewWithMemoryWorker returns the HTTP handler and the background memory worker
// on one shared server, so the worker's sweep and the request path's refreshes
// hold the same single-flight guard.
func NewWithMemoryWorker(d Deps) (http.Handler, *MemoryWorker) {
	handler, memory, _ := NewWithWorkers(d)
	return handler, memory
}

// NewWithWorkers is NewWithMemoryWorker plus the worker that starts due
// scheduled runs, on the same server.
func NewWithWorkers(d Deps) (http.Handler, *MemoryWorker, *ScheduleWorker) {
	s := newServer(d)

	mux := http.NewServeMux()
	httpx.Health(mux, slog.Default())
	mux.HandleFunc("GET /api/model", s.handleModel)
	mux.HandleFunc("GET /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /api/auth/callback", s.handleAuthCallback)
	mux.Handle("POST /api/auth/logout", s.requireAuth(http.HandlerFunc(s.handleAuthLogout)))
	mux.Handle("GET /api/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	mux.Handle("PATCH /api/me", s.requireAuth(http.HandlerFunc(s.handleUpdateMe)))
	mux.Handle("GET /api/me/usage", s.requireAuth(http.HandlerFunc(s.handleGetUsage)))
	mux.Handle("GET /api/me/memory", s.requireAuth(http.HandlerFunc(s.handleGetUserMemory)))
	mux.Handle("GET /api/me/directives", s.requireAuth(http.HandlerFunc(s.handleGetUserDirectives)))
	mux.Handle("GET /api/mcp/servers", s.requireAuth(http.HandlerFunc(s.handleMCPServers)))
	mux.Handle("GET /api/mcp/tools", s.requireAuth(http.HandlerFunc(s.handleMCPTools)))
	mux.Handle("GET /api/admin/users", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminUsers))))
	mux.Handle("GET /api/projects", s.requireAuth(http.HandlerFunc(s.handleListProjects)))
	mux.Handle("POST /api/projects", s.requireAuth(http.HandlerFunc(s.handleCreateProject)))
	mux.Handle("PATCH /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.handleUpdateProject)))
	mux.Handle("POST /api/projects/{projectID}/star", s.requireAuth(http.HandlerFunc(s.handleStarProject)))
	mux.Handle("POST /api/projects/{projectID}/unstar", s.requireAuth(http.HandlerFunc(s.handleUnstarProject)))
	mux.Handle("POST /api/projects/{projectID}/archive", s.requireAuth(http.HandlerFunc(s.handleArchiveProject)))
	mux.Handle("POST /api/projects/{projectID}/unarchive", s.requireAuth(http.HandlerFunc(s.handleUnarchiveProject)))
	mux.Handle("DELETE /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.handleDeleteProject)))
	mux.Handle("GET /api/projects/{projectID}/memory", s.requireAuth(http.HandlerFunc(s.handleGetProjectMemory)))
	mux.Handle("POST /api/projects/{projectID}/memory:edit", s.requireAuth(http.HandlerFunc(s.handleEditProjectMemory)))
	mux.Handle("GET /api/threads", s.requireAuth(http.HandlerFunc(s.handleListThreads)))
	mux.Handle("POST /api/threads", s.requireAuth(http.HandlerFunc(s.handleCreateThread)))
	mux.Handle("POST /api/threads:delete", s.requireAuth(http.HandlerFunc(s.handleBulkDeleteThreads)))
	mux.Handle("GET /api/threads/ids", s.requireAuth(http.HandlerFunc(s.handleListThreadIDs)))
	mux.Handle("GET /api/threads/search", s.requireAuth(http.HandlerFunc(s.handleSearchThreadContent)))
	mux.Handle("GET /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleGetThread)))
	mux.Handle("PATCH /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleUpdateThread)))
	mux.Handle("POST /api/threads/{threadID}/star", s.requireAuth(http.HandlerFunc(s.handleStarThread)))
	mux.Handle("POST /api/threads/{threadID}/unstar", s.requireAuth(http.HandlerFunc(s.handleUnstarThread)))
	mux.Handle("POST /api/threads/{threadID}/archive", s.requireAuth(http.HandlerFunc(s.handleArchiveThread)))
	mux.Handle("POST /api/threads/{threadID}/unarchive", s.requireAuth(http.HandlerFunc(s.handleUnarchiveThread)))
	mux.Handle("DELETE /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleDeleteThread)))
	mux.Handle("POST /api/threads/{threadID}/messages:stream", s.requireAuth(http.HandlerFunc(s.handleStreamMessage)))
	mux.Handle("POST /api/threads/{threadID}/messages:stop", s.requireAuth(http.HandlerFunc(s.handleStopStreamMessage)))
	mux.Handle("GET /api/threads/{threadID}/messages:attach", s.requireAuth(http.HandlerFunc(s.handleAttachStreamMessage)))
	mux.Handle("POST /api/threads/{threadID}/messages:queue", s.requireAuth(http.HandlerFunc(s.handleQueueMessage)))
	mux.Handle("DELETE /api/threads/{threadID}/messages:queue/{clientMessageID}", s.requireAuth(http.HandlerFunc(s.handleDeleteQueuedMessage)))
	// Incognito: a fully ephemeral, stateless turn — no thread/messages/artifacts
	// rows, no memory reads or writes. History is supplied by the client per request.
	mux.Handle("POST /api/incognito/messages:stream", s.requireAuth(http.HandlerFunc(s.handleIncognitoStreamMessage)))
	mux.Handle("POST /api/threads/{threadID}/share", s.requireAuth(http.HandlerFunc(s.handleCreateShare)))
	mux.Handle("POST /api/threads/{threadID}/share:update", s.requireAuth(http.HandlerFunc(s.handleUpdateShare)))
	mux.Handle("DELETE /api/threads/{threadID}/share", s.requireAuth(http.HandlerFunc(s.handleDisableShare)))
	mux.Handle("GET /api/shares", s.requireAuth(http.HandlerFunc(s.handleListMyShares)))
	// Public, UNAUTHENTICATED share endpoints. The snapshot is pre-sanitized and the
	// artifact endpoints serve only ids in the share's allowlist; see share_handlers.go.
	mux.HandleFunc("GET /api/shares/{shareID}", s.handleGetPublicShare)
	mux.HandleFunc("GET /api/shares/{shareID}/artifacts/{artifactID}/download", s.handlePublicShareArtifactDownload)
	mux.HandleFunc("GET /api/shares/{shareID}/artifacts/{artifactID}/thumbnail", s.handlePublicShareArtifactThumbnail)
	mux.Handle("GET /api/schedules", s.requireAuth(http.HandlerFunc(s.handleListSchedules)))
	mux.Handle("POST /api/schedules", s.requireAuth(http.HandlerFunc(s.handleCreateSchedule)))
	mux.Handle("POST /api/schedules:parse", s.requireAuth(http.HandlerFunc(s.handleParseSchedule)))
	mux.Handle("GET /api/schedules/{scheduleID}", s.requireAuth(http.HandlerFunc(s.handleGetSchedule)))
	mux.Handle("PATCH /api/schedules/{scheduleID}", s.requireAuth(http.HandlerFunc(s.handleUpdateSchedule)))
	mux.Handle("DELETE /api/schedules/{scheduleID}", s.requireAuth(http.HandlerFunc(s.handleDeleteSchedule)))
	mux.Handle("POST /api/schedules/{scheduleID}/run", s.requireAuth(http.HandlerFunc(s.handleRunSchedule)))
	mux.Handle("GET /api/schedules/{scheduleID}/runs", s.requireAuth(http.HandlerFunc(s.handleListScheduleRuns)))
	mux.Handle("GET /api/schedules/{scheduleID}/memory", s.requireAuth(http.HandlerFunc(s.handleGetScheduleMemory)))
	mux.Handle("DELETE /api/schedules/{scheduleID}/memory/{kind}", s.requireAuth(http.HandlerFunc(s.handleDeleteScheduleMemory)))
	mux.Handle("GET /api/notifications", s.requireAuth(http.HandlerFunc(s.handleListNotifications)))
	mux.Handle("GET /api/notifications/unread", s.requireAuth(http.HandlerFunc(s.handleUnreadNotifications)))
	mux.Handle("POST /api/notifications/read-all", s.requireAuth(http.HandlerFunc(s.handleReadAllNotifications)))
	mux.Handle("POST /api/notifications/{notificationID}/read", s.requireAuth(http.HandlerFunc(s.handleReadNotification)))
	mux.Handle("GET /api/favicon", s.requireAuth(http.HandlerFunc(s.handleFavicon)))
	mux.Handle("GET /api/source-image", s.requireAuth(http.HandlerFunc(s.handleSourceImage)))
	mux.Handle("GET /api/artifacts", s.requireAuth(http.HandlerFunc(s.handleListArtifacts)))
	mux.Handle("POST /api/artifacts/images/upload", s.requireAuth(http.HandlerFunc(s.handleUploadImageAttachment)))
	mux.Handle("GET /api/artifacts/{artifactID}/download", s.requireAuth(http.HandlerFunc(s.handleDownloadArtifact)))
	mux.Handle("GET /api/artifacts/{artifactID}/thumbnail", s.requireAuth(http.HandlerFunc(s.handleThumbnailArtifact)))
	mux.Handle("PATCH /api/artifacts/{artifactID}", s.requireAuth(http.HandlerFunc(s.handleRenameArtifact)))
	mux.Handle("DELETE /api/artifacts/{artifactID}", s.requireAuth(http.HandlerFunc(s.handleDeleteArtifact)))
	mux.Handle("POST /api/documents/upload", s.requireAuth(http.HandlerFunc(s.handleUploadDocument)))
	mux.Handle("GET /api/documents", s.requireAuth(http.HandlerFunc(s.handleListDocuments)))
	mux.Handle("POST /api/documents/{documentID}/index", s.requireAuth(http.HandlerFunc(s.handleIndexDocument)))
	mux.Handle("DELETE /api/documents/{documentID}", s.requireAuth(http.HandlerFunc(s.handleDeleteDocument)))
	if d.Static != nil {
		// Server-render per-share OG/Twitter meta into index.html so link-preview
		// bots get a rich card. More specific than the "/" catch-all, so only the
		// exact single-segment share URL is intercepted; assets/other routes still
		// hit d.Static. See handleShareIndex in share_handlers.go.
		mux.HandleFunc("GET /share/{shareID}", s.handleShareIndex)
		mux.Handle("/", d.Static)
	}

	return middleware(mux), &MemoryWorker{s: s}, &ScheduleWorker{s: s}
}

type activeStreamRegistry struct {
	mu      sync.Mutex
	streams map[activeStreamKey]*activeStream
	// earlyStops holds a stop for a send whose turn has not registered yet: it
	// is still being set up. The turn runs detached from its client, so the
	// client's dropped fetch no longer ends it; register applies the stop
	// instead. Keyed by the send id, a stop can only ever end the turn of the
	// send it was meant for.
	earlyStops map[earlyStopKey]earlyStop
	// queueLocks are the per-thread queue locks; see queueLock.
	queueLocks map[activeStreamKey]*sync.Mutex
	// now is time.Now; tests replace it.
	now func() time.Time
}

type earlyStopKey struct {
	activeStreamKey
	sendID string
}

// earlyStop is a stop waiting for its turn to register.
type earlyStop struct {
	cause error
	at    time.Time
}

// earlyStopWindow bounds how long an early stop waits for its turn. Setup
// takes seconds at most; an older stop is for a send that never registers,
// and is dropped.
const earlyStopWindow = 30 * time.Second

// pruneEarlyStopsLocked drops the early stops past earlyStopWindow.
func (r *activeStreamRegistry) pruneEarlyStopsLocked() {
	now := r.clock()
	for key, early := range r.earlyStops {
		if now.Sub(early.at) >= earlyStopWindow {
			delete(r.earlyStops, key)
		}
	}
}

func (r *activeStreamRegistry) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

type activeStreamKey struct {
	userID   string
	threadID string
}

type activeStream struct {
	key    activeStreamKey
	cancel context.CancelCauseFunc
	// sendID is the client's id for the send that started the turn; "" when
	// the client sent none or the queue started it.
	sendID string
	// hub carries the turn's events to the clients that reattach to it.
	hub *turnHub
	// finishing is set once the stream will run no further turn: its last
	// look at the queue found nothing, or it was stopped or superseded.
	// handedOff follows once its turns have fully ended and it passed what is
	// queued to a stream of its own. A message queued while a stream is
	// finishing but not handed off yet waits for that hand-off, so it never
	// runs beside a stopped turn that is still unwinding. Both under mu.
	finishing bool
	handedOff bool
	// done is closed once the stream handler has returned. stopAndWait blocks on
	// it so a caller that needs the turn to be really finished — the thread delete
	// path, which must not race the turn's remaining writes — can wait for it.
	done      chan struct{}
	closeOnce sync.Once
}

func (r *activeStreamRegistry) register(userID, threadID, sendID string, cancel context.CancelCauseFunc, hub *turnHub) func() {
	_, unregister := r.registerStream(userID, threadID, sendID, cancel, hub)
	return unregister
}

// registerStream is register returning the stream too, for the turn chain
// that later asks finishIfIdle about it.
func (r *activeStreamRegistry) registerStream(userID, threadID, sendID string, cancel context.CancelCauseFunc, hub *turnHub) (*activeStream, func()) {
	key := activeStreamKey{userID: userID, threadID: threadID}
	r.mu.Lock()
	stream, unregister := r.registerLocked(key, sendID, cancel, hub)
	r.pruneEarlyStopsLocked()
	var early earlyStop
	hasEarly := false
	if sendID != "" {
		earlyKey := earlyStopKey{activeStreamKey: key, sendID: sendID}
		early, hasEarly = r.earlyStops[earlyKey]
		delete(r.earlyStops, earlyKey)
	}
	r.mu.Unlock()
	if hasEarly {
		cancel(early.cause)
	}
	return stream, unregister
}

// registerLocked makes a new stream the thread's running one. A previous one
// still running is superseded; one already finishing is left to end on its
// own, as it starts no further turn.
func (r *activeStreamRegistry) registerLocked(key activeStreamKey, sendID string, cancel context.CancelCauseFunc, hub *turnHub) (*activeStream, func()) {
	stream := &activeStream{key: key, cancel: cancel, sendID: sendID, hub: hub, done: make(chan struct{})}
	if r.streams == nil {
		r.streams = make(map[activeStreamKey]*activeStream)
	}
	if previous := r.streams[key]; previous != nil && !previous.finishing {
		previous.finishing = true
		previous.cancel(errStreamSuperseded)
	}
	r.streams[key] = stream
	return stream, func() {
		r.mu.Lock()
		if r.streams[key] == stream {
			delete(r.streams, key)
		}
		r.mu.Unlock()
		// Closed unconditionally, outside the map check: stop and stopAndWait drop
		// the entry before the handler unwinds, so a close guarded by that check
		// would never fire and every waiter would burn its full timeout.
		stream.closeOnce.Do(func() { close(stream.done) })
	}
}

// queueLock returns the thread's queue lock. It serializes everything that
// reads or moves the thread's queued messages against a decision about who
// answers them: enqueue, a stream's last look, consume, the stop's drain and
// the hand-off. It is per thread, so the database work done under it never
// holds up the registry for other threads.
func (r *activeStreamRegistry) queueLock(key activeStreamKey) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queueLocks == nil {
		r.queueLocks = make(map[activeStreamKey]*sync.Mutex)
	}
	lock := r.queueLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		r.queueLocks[key] = lock
	}
	return lock
}

// withQueueLock runs fn under the thread's queue lock.
func (r *activeStreamRegistry) withQueueLock(userID, threadID string, fn func()) {
	lock := r.queueLock(activeStreamKey{userID: userID, threadID: threadID})
	lock.Lock()
	defer lock.Unlock()
	fn()
}

// enqueue runs insert, which stores a queued message, under the thread's
// queue lock. When no stream will pick the message up, it registers a new one
// with cancel and hub and returns it; the caller then drives that stream. nil
// means a stream has it: the running one, or one finishing that hands it off.
func (r *activeStreamRegistry) enqueue(userID, threadID string, insert func() error, cancel context.CancelCauseFunc, hub *turnHub) (*activeStream, func(), error) {
	key := activeStreamKey{userID: userID, threadID: threadID}
	lock := r.queueLock(key)
	lock.Lock()
	defer lock.Unlock()
	if err := insert(); err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.streams[key]; current != nil && !current.handedOff {
		return nil, nil, nil
	}
	stream, unregister := r.registerLocked(key, "", cancel, hub)
	return stream, unregister, nil
}

// isFinishing reports whether stream will run no further turn.
func (r *activeStreamRegistry) isFinishing(stream *activeStream) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return stream.finishing
}

// finishIfIdle is a stream's look at the queue between turns. It reports
// true, and marks the stream finishing, when the stream is to end: it was
// stopped, superseded or canceled, or hasQueued finds nothing. Under the
// queue lock, so no message is accepted between the look and the decision.
func (r *activeStreamRegistry) finishIfIdle(stream *activeStream, canceled bool, hasQueued func() bool) bool {
	lock := r.queueLock(stream.key)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	if stream.finishing || canceled || r.streams[stream.key] != stream {
		stream.finishing = true
		r.mu.Unlock()
		return true
	}
	r.mu.Unlock()
	has := hasQueued()
	r.mu.Lock()
	defer r.mu.Unlock()
	// A stop may have come in during the look.
	if !has || stream.finishing {
		stream.finishing = true
		return true
	}
	return false
}

// consume runs fn, which moves the queued messages into the thread, unless
// the stream was stopped: a stop hands them back instead (see the stop
// handler's drain, under the same lock).
func (r *activeStreamRegistry) consume(stream *activeStream, fn func() ([]chat.Message, error)) ([]chat.Message, error) {
	lock := r.queueLock(stream.key)
	lock.Lock()
	defer lock.Unlock()
	if r.isFinishing(stream) {
		return nil, nil
	}
	return fn()
}

// handOff is a finished stream's last act, after its turns have fully ended:
// what was queued meanwhile goes to a stream of its own, which start
// registers (with registerHandOff) and drives. Nothing starts when a newer
// stream owns the thread; it reads the queue itself.
func (r *activeStreamRegistry) handOff(stream *activeStream, hasQueued func() bool, start func()) {
	lock := r.queueLock(stream.key)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	stream.finishing = true
	stream.handedOff = true
	current := r.streams[stream.key] == stream
	r.mu.Unlock()
	if current && hasQueued() {
		start()
	}
}

// abandon ends a stream that never ran, without a hand-off: the server is
// shutting down. Registry lock only, so it is safe inside a hand-off.
func (r *activeStreamRegistry) abandon(stream *activeStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stream.finishing = true
	stream.handedOff = true
}

// registerHandOff registers the stream a hand-off starts. The caller holds
// the thread's queue lock (see handOff).
func (r *activeStreamRegistry) registerHandOff(userID, threadID string, cancel context.CancelCauseFunc, hub *turnHub) (*activeStream, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registerLocked(activeStreamKey{userID: userID, threadID: threadID}, "", cancel, hub)
}

// lookup returns the hub of the user's running turn on the thread, or nil when
// none is running.
func (r *activeStreamRegistry) lookup(userID, threadID string) *turnHub {
	r.mu.Lock()
	defer r.mu.Unlock()
	if stream := r.streams[activeStreamKey{userID: userID, threadID: threadID}]; stream != nil {
		return stream.hub
	}
	return nil
}

// stop ends the user's running turn on the thread. With a sendID it ends only
// the turn of that send: when that turn has not registered yet, the stop is
// kept for it (see earlyStops) and stop reports false. Without one it ends
// whatever turn is running.
func (r *activeStreamRegistry) stop(userID, threadID, sendID string, cause error) bool {
	key := activeStreamKey{userID: userID, threadID: threadID}
	r.mu.Lock()
	stream := r.streams[key]
	// A stream the queue started has no send of its own: the client
	// following the thread stops it with whatever send it knows.
	if stream != nil && sendID != "" && stream.sendID != "" && stream.sendID != sendID {
		stream = nil
	}
	if stream == nil && sendID != "" {
		r.pruneEarlyStopsLocked()
		if r.earlyStops == nil {
			r.earlyStops = make(map[earlyStopKey]earlyStop)
		}
		r.earlyStops[earlyStopKey{activeStreamKey: key, sendID: sendID}] = earlyStop{cause: cause, at: r.clock()}
	}
	if stream != nil {
		// A stopped stream picks up nothing more; what is queued now starts
		// its own.
		stream.finishing = true
	}
	r.mu.Unlock()
	if stream == nil {
		return false
	}
	// The entry stays until the handler's unregister removes it. Dropping it here
	// would hide a stream that is still unwinding — its uninterruptible persist
	// block runs on a detached context — from a stopAndWait that arrives right
	// after a stop, and that delete would then race those writes instead of
	// waiting for them. Cancelling an already-canceled context is a no-op.
	stream.cancel(cause)
	return true
}

// stopAndDrain is stop and, when it found the stream, drain, which hands the
// queued messages back, as one step under the thread's queue lock. Apart, the
// canceled turn could unwind in between: its hand-off still found the
// messages queued and started a stream that answered them, and the stop
// handed back nothing.
func (r *activeStreamRegistry) stopAndDrain(userID, threadID, sendID string, cause error, drain func()) bool {
	lock := r.queueLock(activeStreamKey{userID: userID, threadID: threadID})
	lock.Lock()
	defer lock.Unlock()
	if !r.stop(userID, threadID, sendID, cause) {
		return false
	}
	drain()
	return true
}

// stopAndWait cancels the user's active stream on the thread and waits for the
// handler goroutine to unwind, bounded by timeout. The delete paths use it so a
// canceled turn's remaining writes land before the thread rows go away, instead of
// failing a foreign key and leaving orphaned artifact files behind. Returns at once
// when no stream is registered for the key.
func (r *activeStreamRegistry) stopAndWait(userID, threadID string, cause error, timeout time.Duration) {
	key := activeStreamKey{userID: userID, threadID: threadID}
	r.mu.Lock()
	stream := r.streams[key]
	r.mu.Unlock()
	if stream == nil {
		return
	}
	stream.cancel(cause)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-stream.done:
	case <-timer.C:
		slog.Warn("stream did not stop within the wait window", "thread_id", threadID)
	}
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
	return s.auth.RequireAuth(next)
}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
	return s.auth.RequireAdmin(next)
}
