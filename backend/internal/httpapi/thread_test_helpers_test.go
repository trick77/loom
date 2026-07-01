package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

var testUser = auth.User{ID: "user_1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "auto"}

func newAuthenticatedServer(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	return newAuthenticatedServerForUser(t, testUser, deps)
}

type fakeArtifactStore struct {
	artifacts []artifact.Artifact
	// deleted, when set, records the ids passed to Delete (value receiver can't
	// mutate the slice, so deletions are tracked through this pointer instead).
	deleted *[]string
}

func (f fakeArtifactStore) Delete(_ context.Context, _ string, artifactID string) error {
	if f.deleted != nil {
		*f.deleted = append(*f.deleted, artifactID)
	}
	return nil
}

func (f fakeArtifactStore) Rename(_ context.Context, userID, artifactID, displayFilename string) error {
	for i := range f.artifacts {
		if f.artifacts[i].UserID == userID && f.artifacts[i].ID == artifactID {
			f.artifacts[i].DisplayFilename = displayFilename
		}
	}
	return nil
}

func (f fakeArtifactStore) SetThumbnailRelPath(_ context.Context, userID, artifactID, relPath string) error {
	for i := range f.artifacts {
		if f.artifacts[i].UserID == userID && f.artifacts[i].ID == artifactID {
			f.artifacts[i].ThumbnailRelPath = relPath
		}
	}
	return nil
}

func (f fakeArtifactStore) GetMany(_ context.Context, userID string, ids []string) (map[string]artifact.Artifact, error) {
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make(map[string]artifact.Artifact)
	for _, item := range f.artifacts {
		if item.UserID != userID {
			continue
		}
		if _, ok := want[item.ID]; ok {
			out[item.ID] = item
		}
	}
	return out, nil
}

func (f fakeArtifactStore) Create(context.Context, artifact.CreateInput) (artifact.Artifact, error) {
	return artifact.Artifact{}, nil
}

func (f fakeArtifactStore) Get(_ context.Context, userID, artifactID string) (artifact.Artifact, bool, error) {
	for _, item := range f.artifacts {
		if item.UserID == userID && item.ID == artifactID {
			return item, true, nil
		}
	}
	return artifact.Artifact{}, false, nil
}

func (f fakeArtifactStore) List(_ context.Context, userID string, opts artifact.ListOptions) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	for _, item := range f.artifacts {
		if item.UserID == userID {
			out = append(out, item)
		}
	}
	return out, nil
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

type fakeThreadStore struct {
	thread                    chat.Thread
	project                   chat.Project
	messages                  []chat.Message
	listThreadsUserID         string
	listThreadsOptions        chat.ListThreadsOptions
	assistantContent          string
	assistantContextErr       error
	lastCitations             json.RawMessage
	lastContentBlocks         json.RawMessage
	lastAttachments           json.RawMessage
	createThreadErr           error
	deleteThreadErr           error
	updateThreadInput         chat.UpdateThreadInput
	updateThreadErr           error
	projectMemory             chat.ProjectMemory
	projectMessageCount       int
	projectDescriptionChanged bool
	// projectThreadTitles is returned by ListProjectThreadTitles; it is the source
	// the auto-description summarizes and the count the refresh gate compares against.
	projectThreadTitles []string
	userMemory          chat.UserMemory
	userMessageCount          int
	// userDirectives backs the directive store stubs; ordered as inserted.
	userDirectives []chat.UserDirective
	// directiveWriteErr, when set, is returned by AddUserDirective /
	// ReplaceUserDirective so tests can exercise the budget-full path.
	directiveWriteErr error
	// listLimit records the limit passed to the most recent ListUserMessages /
	// ListProjectMessages call, so tests can assert the adaptive fold window.
	listLimit int
	// shares maps threadID -> the thread's share row, for share handler tests.
	shares map[string]chat.Share
	// searchHits is returned verbatim by SearchMessages, for conversation_search tests.
	searchHits []chat.MessageSearchHit
	// contentHits is returned verbatim by SearchThreadsByContent, for the
	// /api/threads/search handler tests.
	contentHits []chat.ThreadContentHit
}

func (f *fakeThreadStore) CreateProject(_ context.Context, userID string, in chat.CreateProjectInput) (chat.Project, error) {
	f.project = chat.Project{ID: "proj_1", UserID: userID, Name: in.Name, Description: in.Description, DescriptionUserEdited: in.Description != ""}
	return f.project, nil
}

func (f *fakeThreadStore) GetProject(_ context.Context, _ string, projectID string) (chat.Project, bool, error) {
	if f.project.ID == "" || f.project.ID != projectID {
		return chat.Project{}, false, nil
	}
	return f.project, true, nil
}

func (f *fakeThreadStore) ListProjects(context.Context, string, bool) ([]chat.Project, error) {
	if f.project.ID == "" {
		return []chat.Project{}, nil
	}
	return []chat.Project{f.project}, nil
}

func (f *fakeThreadStore) UpdateProject(_ context.Context, _ string, projectID string, _ chat.UpdateProjectInput) (chat.Project, bool, error) {
	if f.project.ID == "" || f.project.ID != projectID {
		return chat.Project{}, false, nil
	}
	return f.project, true, nil
}

func (f *fakeThreadStore) SetAutoProjectDescription(_ context.Context, _ string, projectID, description string, sourceThreadCount int) (chat.Project, bool, error) {
	if f.project.ID == "" || f.project.ID != projectID {
		return chat.Project{}, false, nil
	}
	// Mirror the real store's atomic lock: a user-edited description is never
	// overwritten by auto-generation.
	if f.project.DescriptionUserEdited {
		return f.project, false, nil
	}
	f.project.Description = description
	f.project.DescriptionSourceThreadCount = sourceThreadCount
	now := time.Now()
	f.project.AutoDescriptionGeneratedAt = &now
	f.projectDescriptionChanged = true
	return f.project, true, nil
}

func (f *fakeThreadStore) ListProjectThreadTitles(_ context.Context, _ string, projectID string) ([]string, error) {
	if f.project.ID == "" || f.project.ID != projectID {
		return nil, nil
	}
	return f.projectThreadTitles, nil
}

func (f *fakeThreadStore) SetProjectStarred(_ context.Context, _ string, projectID string, starred bool) (chat.Project, bool, error) {
	if f.project.ID == "" || f.project.ID != projectID {
		return chat.Project{}, false, nil
	}
	f.project.Starred = starred
	return f.project, true, nil
}

func (f *fakeThreadStore) SetProjectArchived(_ context.Context, _ string, projectID string, _ bool) (bool, error) {
	return f.project.ID != "" && f.project.ID == projectID, nil
}

func (f *fakeThreadStore) DeleteProject(_ context.Context, _ string, projectID string) (bool, error) {
	return f.project.ID != "" && f.project.ID == projectID, nil
}

func (f *fakeThreadStore) CreateThread(_ context.Context, userID string, in chat.CreateThreadInput) (chat.Thread, error) {
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

func (f *fakeThreadStore) GetThread(context.Context, string, string) (chat.Thread, bool, error) {
	if f.thread.ID == "" {
		return chat.Thread{}, false, nil
	}
	return f.thread, true, nil
}

func (f *fakeThreadStore) ListThreads(_ context.Context, userID string, opts chat.ListThreadsOptions) ([]chat.Thread, error) {
	f.listThreadsUserID = userID
	f.listThreadsOptions = opts
	if f.thread.ID == "" {
		return []chat.Thread{}, nil
	}
	return []chat.Thread{f.thread}, nil
}

func (f *fakeThreadStore) ListThreadIDs(_ context.Context, userID string, opts chat.ListThreadsOptions) ([]string, error) {
	f.listThreadsUserID = userID
	f.listThreadsOptions = opts
	if f.thread.ID == "" {
		return []string{}, nil
	}
	return []string{f.thread.ID}, nil
}

func (f *fakeThreadStore) UpdateThread(_ context.Context, userID, threadID string, in chat.UpdateThreadInput) (chat.Thread, bool, error) {
	f.updateThreadInput = in
	if f.updateThreadErr != nil {
		return chat.Thread{}, false, f.updateThreadErr
	}
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
	if in.ProjectID.Set {
		f.thread.ProjectID = in.ProjectID.Value
	}
	return f.thread, true, nil
}

func (f *fakeThreadStore) SetThreadStarred(context.Context, string, string, bool) (chat.Thread, bool, error) {
	return f.thread, true, nil
}

func (f *fakeThreadStore) SetThreadImageModelIfEmpty(_ context.Context, _, _, model string) (chat.Thread, bool, error) {
	if model != "" && f.thread.ImageModel == "" {
		f.thread.ImageModel = model
		return f.thread, true, nil
	}
	return f.thread, false, nil
}

func (f *fakeThreadStore) SetThreadArchived(context.Context, string, string, bool) (bool, error) {
	return true, nil
}

func (f *fakeThreadStore) DeleteThread(context.Context, string, string) (bool, error) {
	if f.deleteThreadErr != nil {
		return false, f.deleteThreadErr
	}
	return true, nil
}

func (f *fakeThreadStore) AddMessage(ctx context.Context, _ string, threadID string, role chat.Role, content string) (chat.Message, error) {
	return f.AddMessageWithUsage(ctx, "", threadID, role, content, chat.MessageTokenUsage{})
}

func (f *fakeThreadStore) AddMessageWithAttachments(ctx context.Context, _ string, threadID string, role chat.Role, content string, attachments json.RawMessage) (chat.Message, error) {
	if len(attachments) == 0 {
		attachments = json.RawMessage("[]")
	}
	f.lastAttachments = attachments
	message := chat.Message{
		ID:            "msg_1",
		ThreadID:      threadID,
		Role:          role,
		Content:       content,
		Artifacts:     json.RawMessage("[]"),
		ActivityTrace: json.RawMessage("[]"),
		Citations:     json.RawMessage("[]"),
		Attachments:   attachments,
	}
	f.messages = append(f.messages, message)
	return message, nil
}

func (f *fakeThreadStore) AddMessageWithUsage(ctx context.Context, _ string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage) (chat.Message, error) {
	return f.AddMessageWithArtifacts(ctx, "", threadID, role, content, usage, nil)
}

func (f *fakeThreadStore) AddMessageWithArtifacts(ctx context.Context, _ string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage, artifacts json.RawMessage) (chat.Message, error) {
	return f.AddMessageWithActivityTrace(ctx, "", threadID, role, content, usage, artifacts, nil)
}

func (f *fakeThreadStore) AddMessageWithActivityTrace(ctx context.Context, userID string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage) (chat.Message, error) {
	return f.AddMessageWithCitations(ctx, userID, threadID, role, content, usage, artifacts, activityTrace, nil, nil)
}

func (f *fakeThreadStore) AddMessageWithCitations(ctx context.Context, _ string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage, citations json.RawMessage, contentBlocks json.RawMessage) (chat.Message, error) {
	if len(artifacts) == 0 {
		artifacts = json.RawMessage("[]")
	}
	if len(activityTrace) == 0 {
		activityTrace = json.RawMessage("[]")
	}
	if len(citations) == 0 {
		citations = json.RawMessage("[]")
	}
	if len(contentBlocks) == 0 {
		contentBlocks = json.RawMessage("[]")
	}
	f.lastCitations = citations
	f.lastContentBlocks = contentBlocks
	message := chat.Message{
		ID:               "msg_1",
		ThreadID:         threadID,
		Role:             role,
		Content:          content,
		ReasoningContent: usage.ReasoningContent,
		Artifacts:        artifacts,
		ActivityTrace:    activityTrace,
		Citations:        citations,
		ContentBlocks:    contentBlocks,
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

func (f *fakeThreadStore) ListMessages(context.Context, string, string) ([]chat.Message, bool, error) {
	return append([]chat.Message(nil), f.messages...), true, nil
}

func (f *fakeThreadStore) ListRecentMessages(_ context.Context, _ string, _ string, limit int) ([]chat.Message, error) {
	msgs := append([]chat.Message(nil), f.messages...)
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

func (f *fakeThreadStore) SearchMessages(_ context.Context, _ string, _ string, _ *string, _ string, _ int) ([]chat.MessageSearchHit, error) {
	return append([]chat.MessageSearchHit(nil), f.searchHits...), nil
}

func (f *fakeThreadStore) SearchThreadsByContent(_ context.Context, _ string, _ string, _ *string, limit int) ([]chat.ThreadContentHit, error) {
	hits := append([]chat.ThreadContentHit(nil), f.contentHits...)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (f *fakeThreadStore) GetProjectMemory(_ context.Context, _ string, projectID string) (chat.ProjectMemory, bool, error) {
	if f.projectMemory.ProjectID == "" {
		return chat.ProjectMemory{ProjectID: projectID}, false, nil
	}
	return f.projectMemory, true, nil
}

func (f *fakeThreadStore) UpsertProjectMemory(_ context.Context, _ string, projectID, content string, sourceMessageCount int) (chat.ProjectMemory, error) {
	f.projectMemory = chat.ProjectMemory{ProjectID: projectID, Content: content, SourceMessageCount: sourceMessageCount}
	return f.projectMemory, nil
}

func (f *fakeThreadStore) CountProjectMessages(context.Context, string, string) (int, error) {
	return f.projectMessageCount, nil
}

func (f *fakeThreadStore) ListProjectMessages(_ context.Context, _ string, _ string, limit int) ([]chat.Message, error) {
	f.listLimit = limit
	return append([]chat.Message(nil), f.messages...), nil
}

func (f *fakeThreadStore) GetUserMemory(context.Context, string) (chat.UserMemory, bool, error) {
	if f.userMemory.Content == "" {
		return chat.UserMemory{}, false, nil
	}
	return f.userMemory, true, nil
}

func (f *fakeThreadStore) UpsertUserMemory(_ context.Context, _ string, content string, sourceMessageCount int) (chat.UserMemory, error) {
	f.userMemory = chat.UserMemory{Content: content, SourceMessageCount: sourceMessageCount}
	return f.userMemory, nil
}

func (f *fakeThreadStore) CountUserMessages(context.Context, string) (int, error) {
	return f.userMessageCount, nil
}

func (f *fakeThreadStore) ListUserMessages(_ context.Context, _ string, limit int) ([]chat.Message, error) {
	f.listLimit = limit
	return append([]chat.Message(nil), f.messages...), nil
}

func (f *fakeThreadStore) ListUserDirectives(context.Context, string) ([]chat.UserDirective, error) {
	return append([]chat.UserDirective(nil), f.userDirectives...), nil
}

func (f *fakeThreadStore) AddUserDirective(_ context.Context, userID, content string) (chat.UserDirective, error) {
	if f.directiveWriteErr != nil {
		return chat.UserDirective{}, f.directiveWriteErr
	}
	directive := chat.UserDirective{ID: "dir_" + strconv.Itoa(len(f.userDirectives)), UserID: userID, Content: content, Position: len(f.userDirectives)}
	f.userDirectives = append(f.userDirectives, directive)
	return directive, nil
}

func (f *fakeThreadStore) RemoveUserDirective(_ context.Context, _, id string) (bool, error) {
	for i, d := range f.userDirectives {
		if d.ID == id {
			f.userDirectives = append(f.userDirectives[:i], f.userDirectives[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeThreadStore) ReplaceUserDirective(_ context.Context, _, id, content string) (chat.UserDirective, bool, error) {
	if f.directiveWriteErr != nil {
		return chat.UserDirective{}, false, f.directiveWriteErr
	}
	for i, d := range f.userDirectives {
		if d.ID == id {
			f.userDirectives[i].Content = content
			return f.userDirectives[i], true, nil
		}
	}
	return chat.UserDirective{}, false, nil
}

func (f *fakeThreadStore) CreateShare(_ context.Context, userID string, in chat.CreateShareInput) (chat.Share, error) {
	if f.shares == nil {
		f.shares = map[string]chat.Share{}
	}
	share := chat.Share{
		ID:          "share-" + in.ThreadID,
		ShareID:     in.ShareID,
		ThreadID:    in.ThreadID,
		UserID:      userID,
		Shared:      true,
		Title:       in.Title,
		Snapshot:    in.Snapshot,
		ArtifactIDs: in.ArtifactIDs,
	}
	f.shares[in.ThreadID] = share
	return share, nil
}

func (f *fakeThreadStore) GetShareByThreadID(_ context.Context, _ string, threadID string) (chat.Share, bool, error) {
	share, ok := f.shares[threadID]
	return share, ok, nil
}

func (f *fakeThreadStore) GetShareByShareID(_ context.Context, shareID string) (chat.Share, bool, error) {
	for _, share := range f.shares {
		if share.ShareID == shareID {
			return share, true, nil
		}
	}
	return chat.Share{}, false, nil
}

func (f *fakeThreadStore) UpdateShareSnapshot(_ context.Context, _ string, threadID string, in chat.UpdateShareInput) (chat.Share, bool, error) {
	share, ok := f.shares[threadID]
	if !ok {
		return chat.Share{}, false, nil
	}
	share.Title = in.Title
	share.Snapshot = in.Snapshot
	share.ArtifactIDs = in.ArtifactIDs
	share.Shared = true
	f.shares[threadID] = share
	return share, true, nil
}

func (f *fakeThreadStore) SetShareEnabled(_ context.Context, _ string, threadID string, enabled bool) (bool, error) {
	share, ok := f.shares[threadID]
	if !ok {
		return false, nil
	}
	share.Shared = enabled
	f.shares[threadID] = share
	return true, nil
}

func (f *fakeThreadStore) ListSharesForUser(_ context.Context, _ string) ([]chat.Share, error) {
	shares := make([]chat.Share, 0, len(f.shares))
	for _, share := range f.shares {
		shares = append(shares, share)
	}
	return shares, nil
}

type fakeChatClient struct {
	title               string
	titleErr            error
	category            string
	reasoningTitle      string
	history             *[]llm.Message
	streamText          *string
	reasoningText       string
	usage               llm.TokenUsage
	titleUsage          llm.TokenUsage
	reasoningTitleUsage llm.TokenUsage
	afterStream         func()
	projectMemory       string
	editedMemory        string
	projectDescription  string
	// projectDescriptionCalls, when set, counts GenerateProjectDescription
	// invocations so a test can assert the in-memory guard short-circuited before
	// any (wasted) inference.
	projectDescriptionCalls *int
	// streamErr, when set, makes StreamChatWithTools emit any reasoning then return
	// the error (no content), modelling a turn that fails/stalls mid-stream.
	streamErr error
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

func (f fakeChatClient) GenerateThreadTitle(ctx context.Context, _, _, _ string) (string, error) {
	if f.titleErr != nil {
		return "", f.titleErr
	}
	// Mirror the real client: a completed helper call records its usage into the
	// request accumulator on ctx.
	llm.RecordUsage(ctx, f.titleUsage)
	return f.title, nil
}

func (f fakeChatClient) ClassifyThread(ctx context.Context, _ string) (string, error) {
	return f.category, nil
}

func (f fakeChatClient) GenerateReasoningTitle(ctx context.Context, _, _ string) (string, error) {
	llm.RecordUsage(ctx, f.reasoningTitleUsage)
	return f.reasoningTitle, nil
}

func (f fakeChatClient) GenerateMemory(_ context.Context, _, _, _, _, _, _ string) (string, error) {
	return f.projectMemory, nil
}

func (f fakeChatClient) ApplyMemoryEdit(_ context.Context, _, _, _, _, _ string) (string, error) {
	return f.editedMemory, nil
}

func (f fakeChatClient) GenerateProjectDescription(_ context.Context, _ string, _ []string, _ string) (string, error) {
	if f.projectDescriptionCalls != nil {
		*f.projectDescriptionCalls++
	}
	return f.projectDescription, nil
}

func (f fakeChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, _ []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	if f.history != nil {
		*f.history = append((*f.history)[:0], history...)
	}
	if f.reasoningText != "" && onEvent != nil {
		if err := onEvent(llm.StreamEvent{ReasoningDelta: f.reasoningText}); err != nil {
			return llm.StreamResult{}, err
		}
	}
	if f.streamErr != nil {
		return llm.StreamResult{ReasoningContent: f.reasoningText}, f.streamErr
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
	llm.RecordUsage(ctx, f.usage)
	return llm.StreamResult{Content: content, ReasoningContent: f.reasoningText, Usage: f.usage}, nil
}

type blockingChatClient struct {
	started        chan struct{}
	done           chan struct{}
	partialContent string
	cancelCause    error
}

func (f *blockingChatClient) StreamChat(ctx context.Context, _ []llm.Message, _ func(string) error) (string, error) {
	result, err := f.StreamChatResult(ctx, nil, nil)
	return result.Content, err
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
	return "", nil
}

func (f *blockingChatClient) ClassifyThread(context.Context, string) (string, error) {
	return "", nil
}

func (f *blockingChatClient) GenerateReasoningTitle(context.Context, string, string) (string, error) {
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

type fakeToolChatClient struct {
	results        []llm.StreamResult
	histories      [][]llm.Message
	tools          [][]llm.Tool
	plain          string
	classifyResult string
	titleResult    string
	titleFor       func(reasoning string) string
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

func (f *fakeToolChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, tools []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	f.histories = append(f.histories, append([]llm.Message(nil), history...))
	f.tools = append(f.tools, append([]llm.Tool(nil), tools...))
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
		llm.RecordUsage(ctx, result.Usage)
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
		// Mirror the real client: announce a pending tool call before the parsed
		// call surfaces, so handler/SSE behavior is exercised faithfully.
		if len(result.ToolCalls) > 0 {
			if err := onEvent(llm.StreamEvent{ToolPending: true}); err != nil {
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
	llm.RecordUsage(ctx, result.Usage)
	return result, nil
}

func (f *fakeToolChatClient) GenerateThreadTitle(context.Context, string, string, string) (string, error) {
	return f.titleResult, nil
}

func (f *fakeToolChatClient) ClassifyThread(context.Context, string) (string, error) {
	return f.classifyResult, nil
}

func (f *fakeToolChatClient) GenerateReasoningTitle(_ context.Context, reasoning, _ string) (string, error) {
	if f.titleFor != nil {
		return f.titleFor(reasoning), nil
	}
	return "", nil
}

func (f *fakeToolChatClient) GenerateMemory(_ context.Context, _, _, _, _, _, _ string) (string, error) {
	return "", nil
}

func (f *fakeToolChatClient) ApplyMemoryEdit(_ context.Context, _, _, _, _, _ string) (string, error) {
	return "", nil
}

func (f *fakeToolChatClient) GenerateProjectDescription(context.Context, string, []string, string) (string, error) {
	return "", nil
}

type fakeMCPService struct {
	tools     []llm.Tool
	result    string
	err       error
	available map[string]bool
	callFunc  func(ctx context.Context, name string, args map[string]any) (string, error)
}

func (f fakeMCPService) Tools() []llm.Tool {
	return f.tools
}

func (f fakeMCPService) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if f.callFunc != nil {
		return f.callFunc(ctx, name, args)
	}
	if f.err != nil {
		return "", f.err
	}
	return f.result, nil
}

func (f fakeMCPService) HasTool(name string) bool {
	return f.available[name]
}

var errFakeTool = errors.New("fake tool failed")
