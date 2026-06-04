package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/spark/internal/artifact"
	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/mcp"
)

var testUser = auth.User{ID: "user_1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "auto"}

func newAuthenticatedChatServer(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	return newAuthenticatedChatServerForUser(t, testUser, deps)
}

type fakeArtifactStore struct {
	artifacts []artifact.Artifact
}

func (f fakeArtifactStore) Create(context.Context, artifact.CreateInput) (artifact.Artifact, error) {
	return artifact.Artifact{}, nil
}

func (f fakeArtifactStore) Get(context.Context, string, string) (artifact.Artifact, bool, error) {
	return artifact.Artifact{}, false, nil
}

func (f fakeArtifactStore) ListForThread(_ context.Context, _ string, threadID string) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	for _, item := range f.artifacts {
		if item.ThreadID == threadID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (f fakeArtifactStore) ListForProject(_ context.Context, _ string, projectID string) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	for _, item := range f.artifacts {
		if item.ProjectID != nil && *item.ProjectID == projectID {
			out = append(out, item)
		}
	}
	return out, nil
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
	thread              chat.Thread
	project             chat.Project
	messages            []chat.Message
	listThreadsUserID   string
	listThreadsOptions  chat.ListThreadsOptions
	assistantContent    string
	assistantContextErr error
	createThreadErr     error
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
	title := chat.NormalizeThreadTitle(in.Title)
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
		title := chat.NormalizeThreadTitle(*in.Title)
		if title == "" {
			return chat.Thread{}, false, errors.New("thread title is required")
		}
		f.thread.Title = title
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

func (f *fakeChatStore) AddMessage(ctx context.Context, _ string, threadID string, role chat.Role, content string) (chat.Message, error) {
	return f.AddMessageWithUsage(ctx, "", threadID, role, content, chat.MessageTokenUsage{})
}

func (f *fakeChatStore) AddMessageWithUsage(ctx context.Context, _ string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage) (chat.Message, error) {
	return f.AddMessageWithArtifacts(ctx, "", threadID, role, content, usage, nil)
}

func (f *fakeChatStore) AddMessageWithArtifacts(ctx context.Context, _ string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage, artifacts json.RawMessage) (chat.Message, error) {
	message := chat.Message{
		ID:               "msg_1",
		ThreadID:         threadID,
		Role:             role,
		Content:          content,
		ReasoningContent: usage.ReasoningContent,
		Artifacts:        artifacts,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		CachedTokens:     usage.CachedTokens,
		ReasoningTokens:  usage.ReasoningTokens,
	}
	if role == chat.RoleAssistant {
		f.assistantContent = content
		f.assistantContextErr = ctx.Err()
		message.ID = "msg_2"
	}
	f.messages = append(f.messages, message)
	return message, nil
}

func (f *fakeChatStore) ListMessages(context.Context, string, string) ([]chat.Message, bool, error) {
	return append([]chat.Message(nil), f.messages...), true, nil
}

type fakeChatClient struct {
	title         string
	titleErr      error
	history       *[]llm.Message
	streamText    *string
	reasoningText string
	usage         llm.TokenUsage
	afterStream   func()
}

func (f fakeChatClient) StreamChat(_ context.Context, history []llm.Message, onDelta func(string) error) (string, error) {
	result, err := f.StreamChatResult(context.Background(), history, onDelta)
	return result.Content, err
}

func (f fakeChatClient) StreamChatResult(_ context.Context, history []llm.Message, onDelta func(string) error) (llm.StreamResult, error) {
	if f.history != nil {
		*f.history = append((*f.history)[:0], history...)
	}
	if err := onDelta("Hel"); err != nil {
		return llm.StreamResult{}, err
	}
	if err := onDelta("lo"); err != nil {
		return llm.StreamResult{}, err
	}
	if f.afterStream != nil {
		f.afterStream()
	}
	if f.streamText != nil {
		return llm.StreamResult{Content: *f.streamText, Usage: f.usage}, nil
	}
	return llm.StreamResult{Content: "Hello", Usage: f.usage}, nil
}

func (f fakeChatClient) GenerateTitle(context.Context, string, string) (string, error) {
	if f.titleErr != nil {
		return "", f.titleErr
	}
	return f.title, nil
}

func (f fakeChatClient) StreamChatWithTools(_ context.Context, history []llm.Message, _ []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	if f.history != nil {
		*f.history = append((*f.history)[:0], history...)
	}
	if f.reasoningText != "" && onEvent != nil {
		if err := onEvent(llm.StreamEvent{ReasoningDelta: f.reasoningText}); err != nil {
			return llm.StreamResult{}, err
		}
	}
	content := "Hello"
	if f.streamText != nil {
		content = *f.streamText
	}
	if onEvent != nil {
		if f.streamText != nil {
			if err := onEvent(llm.StreamEvent{Delta: content}); err != nil {
				return llm.StreamResult{}, err
			}
		} else {
			for _, delta := range []string{"Hel", "lo"} {
				if err := onEvent(llm.StreamEvent{Delta: delta}); err != nil {
					return llm.StreamResult{}, err
				}
			}
		}
	}
	if f.afterStream != nil {
		f.afterStream()
	}
	return llm.StreamResult{Content: content, ReasoningContent: f.reasoningText, Usage: f.usage}, nil
}

type blockingChatClient struct {
	started        chan struct{}
	done           chan struct{}
	partialContent string
}

func (f *blockingChatClient) StreamChat(ctx context.Context, _ []llm.Message, _ func(string) error) (string, error) {
	result, err := f.StreamChatResult(ctx, nil, nil)
	return result.Content, err
}

func (f *blockingChatClient) StreamChatResult(ctx context.Context, _ []llm.Message, _ func(string) error) (llm.StreamResult, error) {
	close(f.started)
	<-ctx.Done()
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

func (f *blockingChatClient) GenerateTitle(context.Context, string, string) (string, error) {
	return "", nil
}

type fakeToolChatClient struct {
	results   []llm.StreamResult
	histories [][]llm.Message
	plain     string
}

func (f *fakeToolChatClient) StreamChat(context.Context, []llm.Message, func(string) error) (string, error) {
	result, err := f.StreamChatResult(context.Background(), nil, nil)
	return result.Content, err
}

func (f *fakeToolChatClient) StreamChatResult(context.Context, []llm.Message, func(string) error) (llm.StreamResult, error) {
	if f.plain == "" {
		return llm.StreamResult{}, nil
	}
	return llm.StreamResult{Content: f.plain}, nil
}

func (f *fakeToolChatClient) StreamChatWithTools(_ context.Context, history []llm.Message, tools []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	f.histories = append(f.histories, append([]llm.Message(nil), history...))
	if len(tools) == 0 {
		result, err := f.StreamChatResult(context.Background(), history, nil)
		if err != nil {
			return llm.StreamResult{}, err
		}
		if onEvent != nil {
			if result.ReasoningContent != "" {
				if err := onEvent(llm.StreamEvent{ReasoningDelta: result.ReasoningContent}); err != nil {
					return llm.StreamResult{}, err
				}
			}
			if result.Content != "" {
				if err := onEvent(llm.StreamEvent{Delta: result.Content}); err != nil {
					return llm.StreamResult{}, err
				}
			}
		}
		return result, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	if onEvent != nil {
		if result.ReasoningContent != "" {
			if err := onEvent(llm.StreamEvent{ReasoningDelta: result.ReasoningContent}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		for _, call := range result.ToolCalls {
			if err := onEvent(llm.StreamEvent{ToolCall: call}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		if result.Content != "" {
			if err := onEvent(llm.StreamEvent{Delta: result.Content}); err != nil {
				return llm.StreamResult{}, err
			}
		}
	}
	return result, nil
}

func (f *fakeToolChatClient) GenerateTitle(context.Context, string, string) (string, error) {
	return "", nil
}

type fakeMCPService struct {
	tools    []llm.Tool
	result   string
	err      error
	statuses []mcp.ServerStatus
}

func (f fakeMCPService) Tools() []llm.Tool {
	return f.tools
}

func (f fakeMCPService) CallTool(context.Context, string, map[string]any) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.result, nil
}

func (f fakeMCPService) ServerStatus(context.Context) []mcp.ServerStatus {
	return f.statuses
}

var errFakeTool = errors.New("fake tool failed")
