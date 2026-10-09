package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/store"
	"github.com/trick77/loom/internal/turn/turntest"
)

// Local names for the shared turn-port fakes in turntest.
type (
	fakeArtifactStore   = turntest.ArtifactStore
	fakeThreadStore     = turntest.ThreadStore
	fakeChatClient      = turntest.ChatClient
	fakeToolChatClient  = turntest.ToolChatClient
	fakeMCPService      = turntest.ToolService
	fakeDocumentService = turntest.DocumentService
	stubDocs            = turntest.RetrievingDocuments
	listDocuments       = turntest.ListingDocuments
	fakeSandbox         = turntest.Sandbox
	stubUsageStore      = turntest.UsageStore
	recordingUsageStore = turntest.RecordingUsageStore
	fakeImageProvider   = turntest.ImageProvider
)

var testUser = auth.User{ID: "user_1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "en"}

// newUserDB opens a migrated test database holding testUser's row.
func newUserDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	return db
}

// New returns the fully wired HTTP handler without its memory worker.
func New(d Deps) http.Handler {
	handler, _ := NewWithMemoryWorker(d)
	return handler
}

func newAuthenticatedServer(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	return newAuthenticatedServerForUser(t, testUser, deps)
}

func newAuthenticatedServerForUser(t *testing.T, user auth.User, deps Deps) http.Handler {
	t.Helper()
	deps.Version = "test"
	deps.Auth = auth.NewMiddleware(
		fakeSessionStore{session: auth.Session{Token: "tok", UserID: user.ID}, ok: true},
		fakeUserStore{user: user, ok: true},
	)
	return New(deps)
}

func authenticatedRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})
	return req
}

type blockingChatClient struct {
	started        chan struct{}
	done           chan struct{}
	partialContent string
	cancelCause    error
	titleCalls     atomic.Int32
}

func (f *blockingChatClient) StreamChatResult(ctx context.Context, _ []llm.Message, _ func(string) error) (llm.StreamResult, error) {
	close(f.started)
	<-ctx.Done()
	f.cancelCause = context.Cause(ctx)
	close(f.done)
	return llm.StreamResult{Content: f.partialContent}, ctx.Err()
}

func (f *blockingChatClient) StreamChatWithTools(ctx context.Context, _ []llm.Message, _ []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	if f.partialContent != "" && onEvent != nil {
		if err := onEvent(llm.StreamEvent{Delta: f.partialContent}); err != nil {
			return llm.StreamResult{}, err
		}
	}
	return f.StreamChatResult(ctx, nil, nil)
}

func (f *blockingChatClient) GenerateThreadTitle(context.Context, string, string, string) (string, error) {
	f.titleCalls.Add(1)
	return "", nil
}

func (f *blockingChatClient) ClassifyThread(context.Context, string) (string, error) {
	return "", nil
}

func (f *blockingChatClient) ClassifyImageIntent(context.Context, string, bool, bool) (llm.ImageIntent, error) {
	return llm.ImageIntent{Action: llm.ImageIntentNone}, nil
}

func (f *blockingChatClient) GenerateReasoningTitle(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *blockingChatClient) GenerateWorkingTitle(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *blockingChatClient) GenerateMemory(context.Context, string, string, string, string, string, string) (string, error) {
	return "", nil
}

func (f *blockingChatClient) ApplyMemoryEdit(context.Context, string, string, string, string, string) (string, error) {
	return "", nil
}

func (f *blockingChatClient) GenerateProjectDescription(context.Context, string, []string, string) (string, error) {
	return "", nil
}

var errFakeTool = errors.New("fake tool failed")
