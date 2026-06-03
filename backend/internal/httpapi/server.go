// Package httpapi builds spark's HTTP handler: JSON/SSE API plus the embedded SPA.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/trick77/spark/internal/artifact"
	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/docgen"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/mcp"
)

// Deps are the dependencies needed to build the server. Grows in later phases
// (store, config, services); for Phase 1 only Version and the static handler.
type Deps struct {
	Version string
	Static  http.Handler // serves the embedded SPA; may be nil in tests

	OIDC                  OIDCService
	Auth                  *auth.Middleware
	Sessions              SessionService
	Users                 UserService
	Chat                  ChatStore
	Artifacts             ArtifactStore
	LLM                   ChatClient
	MCP                   ToolService
	DocTools              []docgen.Generator
	UsersDir              string
	OIDCAdminGroup        string
	DevAuthClaims         auth.Claims
	PostLogoutRedirectURL string
}

type server struct {
	version               string
	oidc                  OIDCService
	auth                  *auth.Middleware
	sessions              SessionService
	users                 UserService
	chat                  ChatStore
	artifacts             ArtifactStore
	llm                   ChatClient
	mcp                   ToolService
	docTools              []docgen.Generator
	usersDir              string
	oidcAdminGroup        string
	devAuthClaims         auth.Claims
	postLogoutRedirectURL string
}

// ChatStore is the chat persistence dependency used by chat handlers.
type ChatStore interface {
	CreateProject(context.Context, string, chat.CreateProjectInput) (chat.Project, error)
	ListProjects(context.Context, string, bool) ([]chat.Project, error)
	UpdateProject(context.Context, string, string, chat.UpdateProjectInput) (chat.Project, bool, error)
	SetProjectArchived(context.Context, string, string, bool) (bool, error)
	DeleteProject(context.Context, string, string) (bool, error)
	CreateThread(context.Context, string, chat.CreateThreadInput) (chat.Thread, error)
	GetThread(context.Context, string, string) (chat.Thread, bool, error)
	ListThreads(context.Context, string, chat.ListThreadsOptions) ([]chat.Thread, error)
	UpdateThread(context.Context, string, string, chat.UpdateThreadInput) (chat.Thread, bool, error)
	SetThreadStarred(context.Context, string, string, bool) (chat.Thread, bool, error)
	SetThreadArchived(context.Context, string, string, bool) (bool, error)
	DeleteThread(context.Context, string, string) (bool, error)
	AddMessage(context.Context, string, string, chat.Role, string) (chat.Message, error)
	AddMessageWithUsage(context.Context, string, string, chat.Role, string, chat.MessageTokenUsage) (chat.Message, error)
	AddMessageWithArtifacts(context.Context, string, string, chat.Role, string, chat.MessageTokenUsage, json.RawMessage) (chat.Message, error)
	ListMessages(context.Context, string, string) ([]chat.Message, bool, error)
}

// ArtifactStore persists and looks up generated artifact metadata.
type ArtifactStore interface {
	Create(context.Context, artifact.CreateInput) (artifact.Artifact, error)
	Get(context.Context, string, string) (artifact.Artifact, bool, error)
	ListForThread(context.Context, string, string) ([]artifact.Artifact, error)
	ListForProject(context.Context, string, string) ([]artifact.Artifact, error)
}

// ChatClient is the LLM dependency used by chat stream handlers.
type ChatClient interface {
	StreamChat(context.Context, []llm.Message, func(string) error) (string, error)
	StreamChatWithTools(context.Context, []llm.Message, []llm.Tool, func(llm.StreamEvent) error) (llm.StreamResult, error)
	StreamChatResult(context.Context, []llm.Message, func(string) error) (llm.StreamResult, error)
	GenerateTitle(context.Context, string, string) (string, error)
}

// ToolService exposes configured MCP tools to chat handlers.
type ToolService interface {
	Tools() []llm.Tool
	CallTool(context.Context, string, map[string]any) (string, error)
	ServerStatus(context.Context) []mcp.ServerStatus
}

// OIDCService is the auth handler dependency for OIDC redirects and callbacks.
type OIDCService interface {
	StartLogin(http.ResponseWriter, *http.Request)
	HandleCallback(*http.Request) (auth.Claims, error)
}

// SessionService is the session dependency used by auth handlers.
type SessionService interface {
	Create(context.Context, string, time.Duration) (auth.Session, error)
	Lookup(context.Context, string) (auth.Session, bool, error)
	Revoke(context.Context, string) error
	CookieFor(string, time.Time) *http.Cookie
	ClearCookie() *http.Cookie
}

// UserService is the user dependency used by auth handlers.
type UserService interface {
	auth.UserLookup
	UpsertFromClaims(context.Context, auth.Claims, string) (auth.User, error)
	ListUsers(context.Context) ([]auth.User, error)
}

// New returns the fully wired HTTP handler.
func New(d Deps) http.Handler {
	s := &server{
		version:               d.Version,
		oidc:                  d.OIDC,
		auth:                  d.Auth,
		sessions:              d.Sessions,
		users:                 d.Users,
		chat:                  d.Chat,
		artifacts:             d.Artifacts,
		llm:                   d.LLM,
		mcp:                   d.MCP,
		docTools:              d.DocTools,
		usersDir:              d.UsersDir,
		oidcAdminGroup:        d.OIDCAdminGroup,
		devAuthClaims:         d.DevAuthClaims,
		postLogoutRedirectURL: d.PostLogoutRedirectURL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/health/stream", s.handleHealthStream)
	mux.HandleFunc("GET /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /api/auth/callback", s.handleAuthCallback)
	mux.Handle("POST /api/auth/logout", s.requireAuth(http.HandlerFunc(s.handleAuthLogout)))
	mux.Handle("GET /api/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	mux.Handle("GET /api/admin/users", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminUsers))))
	mux.Handle("GET /api/mcp/status", s.requireAuth(http.HandlerFunc(s.handleMCPStatus)))
	mux.Handle("GET /api/projects", s.requireAuth(http.HandlerFunc(s.handleListProjects)))
	mux.Handle("POST /api/projects", s.requireAuth(http.HandlerFunc(s.handleCreateProject)))
	mux.Handle("PATCH /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.handleUpdateProject)))
	mux.Handle("POST /api/projects/{projectID}/archive", s.requireAuth(http.HandlerFunc(s.handleArchiveProject)))
	mux.Handle("POST /api/projects/{projectID}/unarchive", s.requireAuth(http.HandlerFunc(s.handleUnarchiveProject)))
	mux.Handle("DELETE /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.handleDeleteProject)))
	mux.Handle("GET /api/threads", s.requireAuth(http.HandlerFunc(s.handleListThreads)))
	mux.Handle("POST /api/threads", s.requireAuth(http.HandlerFunc(s.handleCreateThread)))
	mux.Handle("GET /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleGetThread)))
	mux.Handle("PATCH /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleUpdateThread)))
	mux.Handle("POST /api/threads/{threadID}/star", s.requireAuth(http.HandlerFunc(s.handleStarThread)))
	mux.Handle("POST /api/threads/{threadID}/unstar", s.requireAuth(http.HandlerFunc(s.handleUnstarThread)))
	mux.Handle("POST /api/threads/{threadID}/archive", s.requireAuth(http.HandlerFunc(s.handleArchiveThread)))
	mux.Handle("POST /api/threads/{threadID}/unarchive", s.requireAuth(http.HandlerFunc(s.handleUnarchiveThread)))
	mux.Handle("DELETE /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleDeleteThread)))
	mux.Handle("POST /api/threads/{threadID}/messages:stream", s.requireAuth(http.HandlerFunc(s.handleStreamMessage)))
	mux.Handle("GET /api/artifacts/{artifactID}/download", s.requireAuth(http.HandlerFunc(s.handleDownloadArtifact)))
	if d.Static != nil {
		mux.Handle("/", d.Static)
	}

	return logging(recovery(mux))
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
	return s.auth.RequireAuth(next)
}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
	return s.auth.RequireAdmin(next)
}
