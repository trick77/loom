package turntest

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// ThreadStore is an in-memory thread store holding one thread and one project.
// It records what the code under test wrote so tests can assert on it.
type ThreadStore struct {
	Thread              chat.Thread
	Project             chat.Project
	Messages            []chat.Message
	ListThreadsUserID   string
	ListThreadsOptions  chat.ListThreadsOptions
	AssistantContent    string
	AssistantContextErr error
	// UserClientMessageID is the send id the last user message was stored with.
	UserClientMessageID       string
	LastCitations             json.RawMessage
	LastContentBlocks         json.RawMessage
	LastAttachments           json.RawMessage
	LastPastedTexts           json.RawMessage
	CreateThreadErr           error
	DeleteThreadErr           error
	DeletedThreads            []string
	UpdateThreadInput         chat.UpdateThreadInput
	UpdateThreadErr           error
	ProjectMemory             chat.ProjectMemory
	ProjectMessageCount       int
	ProjectDescriptionChanged bool
	// ProjectThreadTitles is returned by ListProjectThreadTitles; it is the source
	// the auto-description summarizes and the count the refresh gate compares against.
	ProjectThreadTitles []string
	UserMemory          chat.UserMemory
	UserMessageCount    int
	// UserDirectives backs the directive store stubs; ordered as inserted.
	UserDirectives []chat.UserDirective
	// DirectiveWriteErr, when set, is returned by AddUserDirective /
	// ReplaceUserDirective so tests can exercise the budget-full path.
	DirectiveWriteErr error
	// ListLimit records the limit passed to the most recent ListUserMessages /
	// ListProjectMessages call, so tests can assert the adaptive fold window.
	ListLimit int
	// Shares maps threadID -> the thread's share row, for share handler tests.
	Shares map[string]chat.Share
	// SearchHits is returned verbatim by SearchMessages, for conversation_search tests.
	SearchHits []chat.MessageSearchHit
	// ContentHits is returned verbatim by SearchThreadsByContent, for the
	// /api/threads/search handler tests.
	ContentHits []chat.ThreadContentHit
}

// CreateProject replaces Project with proj_1.
func (f *ThreadStore) CreateProject(_ context.Context, userID string, in chat.CreateProjectInput) (chat.Project, error) {
	f.Project = chat.Project{ID: "proj_1", UserID: userID, Name: in.Name, Description: in.Description, DescriptionUserEdited: in.Description != ""}
	return f.Project, nil
}

// GetProject returns Project when the id matches.
func (f *ThreadStore) GetProject(_ context.Context, _ string, projectID string) (chat.Project, bool, error) {
	if f.Project.ID == "" || f.Project.ID != projectID {
		return chat.Project{}, false, nil
	}
	return f.Project, true, nil
}

// ListProjects returns Project, if any.
func (f *ThreadStore) ListProjects(context.Context, string, bool) ([]chat.Project, error) {
	if f.Project.ID == "" {
		return []chat.Project{}, nil
	}
	return []chat.Project{f.Project}, nil
}

// UpdateProject returns Project unchanged when the id matches.
func (f *ThreadStore) UpdateProject(_ context.Context, _ string, projectID string, _ chat.UpdateProjectInput) (chat.Project, bool, error) {
	if f.Project.ID == "" || f.Project.ID != projectID {
		return chat.Project{}, false, nil
	}
	return f.Project, true, nil
}

// SetAutoProjectDescription stores a generated description on Project.
func (f *ThreadStore) SetAutoProjectDescription(_ context.Context, _ string, projectID, description string, sourceThreadCount int) (chat.Project, bool, error) {
	if f.Project.ID == "" || f.Project.ID != projectID {
		return chat.Project{}, false, nil
	}
	// Mirror the real store's atomic lock: a user-edited description is never
	// overwritten by auto-generation.
	if f.Project.DescriptionUserEdited {
		return f.Project, false, nil
	}
	f.Project.Description = description
	f.Project.DescriptionSourceThreadCount = sourceThreadCount
	now := time.Now()
	f.Project.AutoDescriptionGeneratedAt = &now
	f.ProjectDescriptionChanged = true
	return f.Project, true, nil
}

// ListProjectThreadTitles returns ProjectThreadTitles for Project.
func (f *ThreadStore) ListProjectThreadTitles(_ context.Context, _ string, projectID string) ([]string, error) {
	if f.Project.ID == "" || f.Project.ID != projectID {
		return nil, nil
	}
	return f.ProjectThreadTitles, nil
}

// SetProjectStarred stars or unstars Project.
func (f *ThreadStore) SetProjectStarred(_ context.Context, _ string, projectID string, starred bool) (chat.Project, bool, error) {
	if f.Project.ID == "" || f.Project.ID != projectID {
		return chat.Project{}, false, nil
	}
	f.Project.Starred = starred
	return f.Project, true, nil
}

// SetProjectArchived reports whether the id is Project's.
func (f *ThreadStore) SetProjectArchived(_ context.Context, _ string, projectID string, _ bool) (bool, error) {
	return f.Project.ID != "" && f.Project.ID == projectID, nil
}

// DeleteProject reports whether the id is Project's.
func (f *ThreadStore) DeleteProject(_ context.Context, _ string, projectID string) (bool, error) {
	return f.Project.ID != "" && f.Project.ID == projectID, nil
}

// CreateThread replaces Thread with thr_1, or fails with CreateThreadErr.
func (f *ThreadStore) CreateThread(_ context.Context, userID string, in chat.CreateThreadInput) (chat.Thread, error) {
	if f.CreateThreadErr != nil {
		return chat.Thread{}, f.CreateThreadErr
	}
	title := chat.NormalizeThreadTitle(in.Title)
	if title == "" {
		title = chat.DefaultThreadTitle
	}
	f.Thread = chat.Thread{ID: "thr_1", UserID: userID, ProjectID: in.ProjectID, Title: title}
	return f.Thread, nil
}

// GetThread implements turn.ThreadStore.
func (f *ThreadStore) GetThread(context.Context, string, string) (chat.Thread, bool, error) {
	if f.Thread.ID == "" {
		return chat.Thread{}, false, nil
	}
	return f.Thread, true, nil
}

// ListThreads implements turn.ThreadStore.
func (f *ThreadStore) ListThreads(_ context.Context, userID string, opts chat.ListThreadsOptions) ([]chat.Thread, error) {
	f.ListThreadsUserID = userID
	f.ListThreadsOptions = opts
	if f.Thread.ID == "" {
		return []chat.Thread{}, nil
	}
	return []chat.Thread{f.Thread}, nil
}

// ListThreadIDs records its arguments and returns Thread's id, if any.
func (f *ThreadStore) ListThreadIDs(_ context.Context, userID string, opts chat.ListThreadsOptions) ([]string, error) {
	f.ListThreadsUserID = userID
	f.ListThreadsOptions = opts
	if f.Thread.ID == "" {
		return []string{}, nil
	}
	return []string{f.Thread.ID}, nil
}

// UpdateThread implements turn.ThreadStore.
func (f *ThreadStore) UpdateThread(_ context.Context, userID, threadID string, in chat.UpdateThreadInput) (chat.Thread, bool, error) {
	f.UpdateThreadInput = in
	if f.UpdateThreadErr != nil {
		return chat.Thread{}, false, f.UpdateThreadErr
	}
	if f.Thread.ID == "" {
		f.Thread = chat.Thread{ID: threadID, UserID: userID, Title: chat.DefaultThreadTitle}
	}
	if in.Title != nil {
		title := chat.NormalizeThreadTitle(*in.Title)
		if title == "" {
			return chat.Thread{}, false, &chat.ValidationError{Msg: "thread title is required"}
		}
		f.Thread.Title = title
	}
	if in.ProjectID.Set {
		f.Thread.ProjectID = in.ProjectID.Value
	}
	return f.Thread, true, nil
}

// SetThreadStarred returns Thread unchanged.
func (f *ThreadStore) SetThreadStarred(context.Context, string, string, bool) (chat.Thread, bool, error) {
	return f.Thread, true, nil
}

// SetThreadTitleIfUnchanged implements turn.ThreadStore.
func (f *ThreadStore) SetThreadTitleIfUnchanged(_ context.Context, _, _, expectedTitle, title string) (chat.Thread, bool, error) {
	if f.Thread.Title != expectedTitle {
		return f.Thread, false, nil
	}
	f.Thread.Title = chat.NormalizeThreadTitle(title)
	return f.Thread, true, nil
}

// SetThreadImageModelIfEmpty implements turn.ThreadStore.
func (f *ThreadStore) SetThreadImageModelIfEmpty(_ context.Context, _, _, model string) (chat.Thread, bool, error) {
	if model != "" && f.Thread.ImageModel == "" {
		f.Thread.ImageModel = model
		return f.Thread, true, nil
	}
	return f.Thread, false, nil
}

// SetThreadArchived always succeeds.
func (f *ThreadStore) SetThreadArchived(context.Context, string, string, bool) (bool, error) {
	return true, nil
}

// DeleteThread records the id in DeletedThreads, or fails with DeleteThreadErr.
func (f *ThreadStore) DeleteThread(_ context.Context, _ string, threadID string) (bool, error) {
	if f.DeleteThreadErr != nil {
		return false, f.DeleteThreadErr
	}
	f.DeletedThreads = append(f.DeletedThreads, threadID)
	return true, nil
}

// AddMessageWithAttachments appends msg_1 to Messages and records its
// attachments, pasted texts and send id.
func (f *ThreadStore) AddMessageWithAttachments(_ context.Context, _ string, threadID string, role chat.Role, content string, attachments json.RawMessage, pastedTexts json.RawMessage, clientMessageID string) (chat.Message, error) {
	f.UserClientMessageID = clientMessageID
	if len(attachments) == 0 {
		attachments = json.RawMessage("[]")
	}
	if len(pastedTexts) == 0 {
		pastedTexts = json.RawMessage("[]")
	}
	f.LastAttachments = attachments
	f.LastPastedTexts = pastedTexts
	message := chat.Message{
		ID:            "msg_1",
		ThreadID:      threadID,
		Role:          role,
		Content:       content,
		Artifacts:     json.RawMessage("[]"),
		ActivityTrace: json.RawMessage("[]"),
		Citations:     json.RawMessage("[]"),
		Attachments:   attachments,
		PastedTexts:   pastedTexts,
	}
	f.Messages = append(f.Messages, message)
	return message, nil
}

// AddMessageCost implements turn.ThreadStore.
func (f *ThreadStore) AddMessageCost(_ context.Context, _ string, messageID string, nanoUSD int64) (bool, error) {
	for i := range f.Messages {
		if f.Messages[i].ID != messageID {
			continue
		}
		total := nanoUSD
		if f.Messages[i].CostNanoUSD != nil {
			total += *f.Messages[i].CostNanoUSD
		}
		f.Messages[i].CostNanoUSD = &total
		return true, nil
	}
	return false, nil
}

// AddMessageWithCitations implements turn.ThreadStore.
func (f *ThreadStore) AddMessageWithCitations(ctx context.Context, _ string, threadID string, role chat.Role, content string, usage chat.MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage, citations json.RawMessage, contentBlocks json.RawMessage) (chat.Message, error) {
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
	f.LastCitations = citations
	f.LastContentBlocks = contentBlocks
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
		CostNanoUSD:      usage.CostNanoUSD,
	}
	if role == chat.RoleAssistant {
		f.AssistantContent = content
		f.AssistantContextErr = ctx.Err()
		message.ID = "msg_2"
	}
	f.Messages = append(f.Messages, message)
	return message, nil
}

// ListMessages returns a copy of Messages.
func (f *ThreadStore) ListMessages(context.Context, string, string) ([]chat.Message, bool, error) {
	return append([]chat.Message(nil), f.Messages...), true, nil
}

// ListRecentMessages implements turn.ThreadStore.
func (f *ThreadStore) ListRecentMessages(_ context.Context, _ string, _ string, limit int) ([]chat.Message, error) {
	msgs := append([]chat.Message(nil), f.Messages...)
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

// ListRecentMessagesForThreads implements turn.ThreadStore.
func (f *ThreadStore) ListRecentMessagesForThreads(ctx context.Context, userID string, threadIDs []string, limit int) (map[string][]chat.Message, error) {
	out := make(map[string][]chat.Message, len(threadIDs))
	for _, id := range threadIDs {
		msgs, err := f.ListRecentMessages(ctx, userID, id, limit)
		if err != nil {
			return nil, err
		}
		out[id] = msgs
	}
	return out, nil
}

// SearchMessages implements turn.ThreadStore.
func (f *ThreadStore) SearchMessages(_ context.Context, _ string, _ string, _ *string, _ string, _ int) ([]chat.MessageSearchHit, error) {
	return append([]chat.MessageSearchHit(nil), f.SearchHits...), nil
}

// SearchThreadsByContent returns ContentHits up to limit.
func (f *ThreadStore) SearchThreadsByContent(_ context.Context, _ string, _ string, _ *string, limit int) ([]chat.ThreadContentHit, error) {
	hits := append([]chat.ThreadContentHit(nil), f.ContentHits...)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// GetProjectMemory returns ProjectMemory, if set.
func (f *ThreadStore) GetProjectMemory(_ context.Context, _ string, projectID string) (chat.ProjectMemory, bool, error) {
	if f.ProjectMemory.ProjectID == "" {
		return chat.ProjectMemory{ProjectID: projectID}, false, nil
	}
	return f.ProjectMemory, true, nil
}

// UpsertProjectMemory replaces ProjectMemory.
func (f *ThreadStore) UpsertProjectMemory(_ context.Context, _ string, projectID, content string, sourceMessageCount int) (chat.ProjectMemory, error) {
	f.ProjectMemory = chat.ProjectMemory{ProjectID: projectID, Content: content, SourceMessageCount: sourceMessageCount}
	return f.ProjectMemory, nil
}

// CountProjectMessages returns ProjectMessageCount.
func (f *ThreadStore) CountProjectMessages(context.Context, string, string) (int, error) {
	return f.ProjectMessageCount, nil
}

// ListProjectMessages records limit in ListLimit and returns Messages.
func (f *ThreadStore) ListProjectMessages(_ context.Context, _ string, _ string, limit int) ([]chat.Message, error) {
	f.ListLimit = limit
	return append([]chat.Message(nil), f.Messages...), nil
}

// GetUserMemory returns UserMemory, if set.
func (f *ThreadStore) GetUserMemory(context.Context, string) (chat.UserMemory, bool, error) {
	if f.UserMemory.Content == "" {
		return chat.UserMemory{}, false, nil
	}
	return f.UserMemory, true, nil
}

// UpsertUserMemory replaces UserMemory.
func (f *ThreadStore) UpsertUserMemory(_ context.Context, _ string, content string, sourceMessageCount int) (chat.UserMemory, error) {
	f.UserMemory = chat.UserMemory{Content: content, SourceMessageCount: sourceMessageCount}
	return f.UserMemory, nil
}

// CountUserMessages returns UserMessageCount.
func (f *ThreadStore) CountUserMessages(context.Context, string) (int, error) {
	return f.UserMessageCount, nil
}

// ListUserMessages records limit in ListLimit and returns Messages.
func (f *ThreadStore) ListUserMessages(_ context.Context, _ string, limit int) ([]chat.Message, error) {
	f.ListLimit = limit
	return append([]chat.Message(nil), f.Messages...), nil
}

// ListUserDirectives implements turn.ThreadStore.
func (f *ThreadStore) ListUserDirectives(context.Context, string) ([]chat.UserDirective, error) {
	return append([]chat.UserDirective(nil), f.UserDirectives...), nil
}

// AddUserDirective implements turn.ThreadStore.
func (f *ThreadStore) AddUserDirective(_ context.Context, userID, content string) (chat.UserDirective, error) {
	if f.DirectiveWriteErr != nil {
		return chat.UserDirective{}, f.DirectiveWriteErr
	}
	directive := chat.UserDirective{ID: "dir_" + strconv.Itoa(len(f.UserDirectives)), UserID: userID, Content: content, Position: len(f.UserDirectives)}
	f.UserDirectives = append(f.UserDirectives, directive)
	return directive, nil
}

// RemoveUserDirective implements turn.ThreadStore.
func (f *ThreadStore) RemoveUserDirective(_ context.Context, _, id string) (bool, error) {
	for i, d := range f.UserDirectives {
		if d.ID == id {
			f.UserDirectives = append(f.UserDirectives[:i], f.UserDirectives[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// ReplaceUserDirective implements turn.ThreadStore.
func (f *ThreadStore) ReplaceUserDirective(_ context.Context, _, id, content string) (chat.UserDirective, bool, error) {
	if f.DirectiveWriteErr != nil {
		return chat.UserDirective{}, false, f.DirectiveWriteErr
	}
	for i, d := range f.UserDirectives {
		if d.ID == id {
			f.UserDirectives[i].Content = content
			return f.UserDirectives[i], true, nil
		}
	}
	return chat.UserDirective{}, false, nil
}

// CreateShare stores a share for the thread in Shares.
func (f *ThreadStore) CreateShare(_ context.Context, userID string, in chat.CreateShareInput) (chat.Share, error) {
	if f.Shares == nil {
		f.Shares = map[string]chat.Share{}
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
	f.Shares[in.ThreadID] = share
	return share, nil
}

// GetShareByThreadID returns the thread's share from Shares.
func (f *ThreadStore) GetShareByThreadID(_ context.Context, _ string, threadID string) (chat.Share, bool, error) {
	share, ok := f.Shares[threadID]
	return share, ok, nil
}

// GetShareByShareID finds a share in Shares by its public id.
func (f *ThreadStore) GetShareByShareID(_ context.Context, shareID string) (chat.Share, bool, error) {
	for _, share := range f.Shares {
		if share.ShareID == shareID {
			return share, true, nil
		}
	}
	return chat.Share{}, false, nil
}

// UpdateShareSnapshot replaces the thread's share snapshot in Shares.
func (f *ThreadStore) UpdateShareSnapshot(_ context.Context, _ string, threadID string, in chat.UpdateShareInput) (chat.Share, bool, error) {
	share, ok := f.Shares[threadID]
	if !ok {
		return chat.Share{}, false, nil
	}
	share.Title = in.Title
	share.Snapshot = in.Snapshot
	share.ArtifactIDs = in.ArtifactIDs
	share.Shared = true
	f.Shares[threadID] = share
	return share, true, nil
}

// SetShareEnabled turns the thread's share in Shares on or off.
func (f *ThreadStore) SetShareEnabled(_ context.Context, _ string, threadID string, enabled bool) (bool, error) {
	share, ok := f.Shares[threadID]
	if !ok {
		return false, nil
	}
	share.Shared = enabled
	f.Shares[threadID] = share
	return true, nil
}

// ListSharesForUser returns every share in Shares.
func (f *ThreadStore) ListSharesForUser(_ context.Context, _ string) ([]chat.Share, error) {
	shares := make([]chat.Share, 0, len(f.Shares))
	for _, share := range f.Shares {
		shares = append(shares, share)
	}
	return shares, nil
}
