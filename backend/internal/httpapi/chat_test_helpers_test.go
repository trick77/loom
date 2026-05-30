package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
)

var testUser = auth.User{ID: "user_1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "auto"}

func newAuthenticatedChatServer(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	return newAuthenticatedChatServerForUser(t, testUser, deps)
}

func newAuthenticatedChatServerForUser(t *testing.T, user auth.User, deps Deps) http.Handler {
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

type fakeChatStore struct {
	thread             chat.Thread
	project            chat.Project
	messages           []chat.Message
	listThreadsUserID  string
	listThreadsOptions chat.ListThreadsOptions
	assistantContent   string
	createThreadErr    error
}

func (f *fakeChatStore) CreateProject(_ context.Context, userID string, in chat.CreateProjectInput) (chat.Project, error) {
	f.project = chat.Project{ID: "proj_1", UserID: userID, Name: in.Name, Description: in.Description}
	return f.project, nil
}

func (f *fakeChatStore) ListProjects(context.Context, string, bool) ([]chat.Project, error) {
	return []chat.Project{}, nil
}

func (f *fakeChatStore) UpdateProject(_ context.Context, _ string, projectID string, _ chat.UpdateProjectInput) (chat.Project, bool, error) {
	if f.project.ID == "" || f.project.ID != projectID {
		return chat.Project{}, false, nil
	}
	return f.project, true, nil
}

func (f *fakeChatStore) SetProjectArchived(_ context.Context, _ string, projectID string, _ bool) (bool, error) {
	return f.project.ID != "" && f.project.ID == projectID, nil
}

func (f *fakeChatStore) DeleteProject(_ context.Context, _ string, projectID string) (bool, error) {
	return f.project.ID != "" && f.project.ID == projectID, nil
}

func (f *fakeChatStore) CreateThread(_ context.Context, userID string, in chat.CreateThreadInput) (chat.Thread, error) {
	if f.createThreadErr != nil {
		return chat.Thread{}, f.createThreadErr
	}
	title := in.Title
	if title == "" {
		title = chat.DefaultThreadTitle
	}
	f.thread = chat.Thread{ID: "thr_1", UserID: userID, ProjectID: in.ProjectID, Title: title}
	return f.thread, nil
}

func (f *fakeChatStore) GetThread(context.Context, string, string) (chat.Thread, bool, error) {
	if f.thread.ID == "" {
		return chat.Thread{}, false, nil
	}
	return f.thread, true, nil
}

func (f *fakeChatStore) ListThreads(_ context.Context, userID string, opts chat.ListThreadsOptions) ([]chat.Thread, error) {
	f.listThreadsUserID = userID
	f.listThreadsOptions = opts
	if f.thread.ID == "" {
		return []chat.Thread{}, nil
	}
	return []chat.Thread{f.thread}, nil
}

func (f *fakeChatStore) UpdateThread(_ context.Context, userID, threadID string, in chat.UpdateThreadInput) (chat.Thread, bool, error) {
	if f.thread.ID == "" {
		f.thread = chat.Thread{ID: threadID, UserID: userID, Title: chat.DefaultThreadTitle}
	}
	if in.Title != nil {
		f.thread.Title = *in.Title
	}
	return f.thread, true, nil
}

func (f *fakeChatStore) SetThreadStarred(context.Context, string, string, bool) (chat.Thread, bool, error) {
	return f.thread, true, nil
}

func (f *fakeChatStore) SetThreadArchived(context.Context, string, string, bool) (bool, error) {
	return true, nil
}

func (f *fakeChatStore) DeleteThread(context.Context, string, string) (bool, error) {
	return true, nil
}

func (f *fakeChatStore) AddMessage(_ context.Context, _ string, threadID string, role chat.Role, content string) (chat.Message, error) {
	message := chat.Message{ID: "msg_1", ThreadID: threadID, Role: role, Content: content}
	if role == chat.RoleAssistant {
		f.assistantContent = content
		message.ID = "msg_2"
	}
	f.messages = append(f.messages, message)
	return message, nil
}

func (f *fakeChatStore) ListMessages(context.Context, string, string) ([]chat.Message, bool, error) {
	return append([]chat.Message(nil), f.messages...), true, nil
}

type fakeChatClient struct {
	title    string
	titleErr error
	history  *[]llm.Message
}

func (f fakeChatClient) StreamChat(_ context.Context, history []llm.Message, onDelta func(string) error) (string, error) {
	if f.history != nil {
		*f.history = append((*f.history)[:0], history...)
	}
	if err := onDelta("Hel"); err != nil {
		return "", err
	}
	if err := onDelta("lo"); err != nil {
		return "", err
	}
	return "Hello", nil
}

func (f fakeChatClient) GenerateTitle(context.Context, string, string) (string, error) {
	if f.titleErr != nil {
		return "", f.titleErr
	}
	return f.title, nil
}
