package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// threadDeleteStopTimeout bounds how long a delete waits for a canceled turn on
// that thread to unwind. Cancellation tears down the upstream model connection
// immediately, so the handler normally returns well inside this; on a timeout the
// delete proceeds anyway rather than failing the user's request.
const threadDeleteStopTimeout = 2 * time.Second

func (s *server) handleListThreads(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	opts, err := listThreadsOptionsFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	threads, err := s.thread.ListThreads(r.Context(), user.ID, opts)
	if err != nil {
		serverError(w, r, err, "list threads failed")
		return
	}
	var nextCursor *string
	if limit := chat.EffectiveThreadLimit(opts.Limit); len(threads) == limit {
		cursor := chat.EncodeThreadCursor(threads[len(threads)-1])
		nextCursor = &cursor
	}
	writeJSON(w, listThreadsResponse{Items: threads, NextCursor: nextCursor})
}

// maxThreadContentSearchResults caps the interactive content search. The sidebar
// modal asks for 20; the Threads page asks for more so "Select all" over an
// active search covers essentially every match (claude.ai shows them uncapped).
const maxThreadContentSearchResults = 200

func (s *server) handleSearchThreadContent(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	query := r.URL.Query()
	var projectID *string
	if id := query.Get("projectId"); id != "" && id != "null" {
		projectID = &id
	}
	limit := maxThreadContentSearchResults
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed < limit {
			limit = parsed
		}
	}
	hits, err := s.thread.SearchThreadsByContent(r.Context(), user.ID, query.Get("q"), projectID, limit)
	if err != nil {
		serverError(w, r, err, "search thread content failed")
		return
	}
	items := make([]threadSearchResult, len(hits))
	for i, hit := range hits {
		items[i] = threadSearchResult{Thread: hit.Thread, Snippet: hit.Snippet}
	}
	writeJSON(w, threadSearchResponse{Items: items})
}

func (s *server) handleListThreadIDs(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	opts, err := listThreadsOptionsFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids, err := s.thread.ListThreadIDs(r.Context(), user.ID, opts)
	if err != nil {
		serverError(w, r, err, "list thread ids failed")
		return
	}
	writeJSON(w, ids)
}

func (s *server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body createThreadRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	thread, err := s.thread.CreateThread(r.Context(), user.ID, chat.CreateThreadInput{
		ProjectID: body.ProjectID,
		Title:     body.Title,
	})
	if err != nil {
		writeMappedThreadStoreError(w, r, err, map[string]int{
			"project not found":        http.StatusNotFound,
			"thread title is too long": http.StatusBadRequest,
		})
		return
	}
	s.recordUsage("thread_created", func() error { return s.usage.IncThreadCreated(r.Context(), user.ID) })
	writeJSONStatus(w, http.StatusCreated, thread)
}

func (s *server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	thread, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	messages, found, err := s.thread.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "list messages failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.refreshMessageArtifacts(r.Context(), user.ID, messages); err != nil {
		serverError(w, r, err, "refresh message artifacts failed")
		return
	}
	share, hasShare, err := s.thread.GetShareByThreadID(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread share failed")
		return
	}
	resp := getThreadResponse{Thread: thread, Messages: messages}
	if hasShare {
		summary := s.shareSummaryOf(share)
		resp.Share = &summary
	}
	writeJSON(w, resp)
}

// refreshMessageArtifacts overlays each message's embedded artifact snapshots with
// the artifacts' current display filename and deleted status, so renames and
// deletes made from the Artifacts library show up in the chat transcript. A no-op
// when the artifact store is unconfigured or no artifacts are referenced.
func (s *server) refreshMessageArtifacts(ctx context.Context, userID string, messages []chat.Message) error {
	if s.artifacts == nil {
		return nil
	}
	ids, err := collectArtifactIDs(messages)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	byID, err := s.artifacts.GetMany(ctx, userID, ids)
	if err != nil {
		return err
	}
	return overlayMessageArtifacts(messages, byID)
}

func (s *server) handleUpdateThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body updateThreadRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	thread, found, err := s.thread.UpdateThread(r.Context(), user.ID, r.PathValue("threadID"), body.toInput())
	if err != nil {
		writeMappedThreadStoreError(w, r, err, map[string]int{
			"thread title is required": http.StatusBadRequest,
			"thread title is too long": http.StatusBadRequest,
			"project not found":        http.StatusNotFound,
		})
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, thread)
}

func (s *server) handleStarThread(w http.ResponseWriter, r *http.Request) {
	s.handleSetThreadStarred(w, r, true)
}

func (s *server) handleUnstarThread(w http.ResponseWriter, r *http.Request) {
	s.handleSetThreadStarred(w, r, false)
}

func (s *server) handleSetThreadStarred(w http.ResponseWriter, r *http.Request, starred bool) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	thread, found, err := s.thread.SetThreadStarred(r.Context(), user.ID, r.PathValue("threadID"), starred)
	if err != nil {
		serverError(w, r, err, "update thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, thread)
}

func (s *server) handleArchiveThread(w http.ResponseWriter, r *http.Request) {
	s.handleSetThreadArchived(w, r, true)
}

func (s *server) handleUnarchiveThread(w http.ResponseWriter, r *http.Request) {
	s.handleSetThreadArchived(w, r, false)
}

func (s *server) handleSetThreadArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	found, err := s.thread.SetThreadArchived(r.Context(), user.ID, r.PathValue("threadID"), archived)
	if err != nil {
		serverError(w, r, err, "update thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleDeleteThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	found, err := s.deleteThreadWithData(r.Context(), user.ID, r.PathValue("threadID"))
	if err != nil {
		serverError(w, r, err, "delete thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteThreadWithData removes a thread and everything that belongs to it: the
// turn still generating on it, its private knowledge documents, its rows (via the
// FK cascade), and its artifact files including their sidecar thumbnails. Shared
// by the single and the bulk delete so the two cannot drift apart.
func (s *server) deleteThreadWithData(ctx context.Context, userID, threadID string) (bool, error) {
	// Terminate a turn still generating on this thread before anything is removed.
	// Otherwise it keeps calling the model and its tools against a thread that no
	// longer exists, then fails the messages/artifacts foreign key on write and
	// leaves artifact files on the volume that the cleanup below cannot see. The
	// registry key carries userID, so this can only ever reach the caller's own
	// stream. It returns immediately for the ids with no live stream, so a large
	// bulk batch does not pay the wait per thread.
	s.activeStreams.stopAndWait(userID, threadID, errStreamThreadDeleted, threadDeleteStopTimeout)
	// Before the artifact snapshot, so the in-use query below sees only the
	// documents that outlive this thread.
	if s.documents != nil {
		if err := s.documents.DeleteThreadData(ctx, userID, threadID); err != nil {
			return false, fmt.Errorf("delete thread knowledge: %w", err)
		}
	}
	// Snapshot before the delete: the FK cascade destroys the artifact rows.
	artifacts, err := s.artifactsForThreadCleanup(ctx, userID, threadID)
	if err != nil {
		return false, fmt.Errorf("list thread artifacts: %w", err)
	}
	// The detach commits before the thread row goes, so a DeleteThread failure
	// leaves those artifacts unlinked from a thread that still exists — they drop
	// out of its artifact list. The two stores hold separate handles and cannot
	// share one transaction, and the detach cannot move after the delete (the
	// cascade would already have fired). Retrying the delete is safe and lands the
	// intended end state: the in-use query then returns nothing and the snapshot no
	// longer carries the detached rows.
	keep, err := s.detachArtifactsInUse(ctx, userID, threadID)
	if err != nil {
		return false, fmt.Errorf("detach in-use thread artifacts: %w", err)
	}
	found, err := s.thread.DeleteThread(ctx, userID, threadID)
	if err != nil || !found {
		return found, err
	}
	s.cleanupArtifactFiles(userID, artifacts, keep)
	return true, nil
}

func (s *server) handleBulkDeleteThreads(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body bulkDeleteThreadsRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	deleted := 0
	seen := make(map[string]struct{}, len(body.ThreadIDs))
	for _, threadID := range body.ThreadIDs {
		if threadID == "" {
			continue
		}
		if _, dup := seen[threadID]; dup {
			continue
		}
		seen[threadID] = struct{}{}

		// Best-effort: skip a thread we cannot clean up or delete rather than
		// aborting the whole batch, which would leave it partially applied.
		// Skips are logged so a silently-dropped thread is still traceable.
		found, err := s.deleteThreadWithData(r.Context(), user.ID, threadID)
		if err != nil {
			slog.Warn("bulk delete: skip thread, delete failed", "thread_id", threadID, "err", err)
			continue
		}
		if !found {
			continue
		}
		deleted++
	}
	writeJSON(w, bulkDeleteThreadsResponse{Deleted: deleted})
}

func listThreadsOptionsFromRequest(r *http.Request) (chat.ListThreadsOptions, error) {
	var opts chat.ListThreadsOptions
	query := r.URL.Query()
	switch projectID := query.Get("projectId"); projectID {
	case "":
	case "null":
		opts.ProjectlessOnly = true
	default:
		opts.ProjectID = &projectID
	}
	starred, err := parseOptionalBool(r, "starred")
	if err != nil {
		return chat.ListThreadsOptions{}, err
	}
	opts.StarredOnly = starred
	archived, err := parseOptionalBool(r, "archived")
	if err != nil {
		return chat.ListThreadsOptions{}, err
	}
	opts.Archived = archived
	limit, err := parseOptionalLimit(r, "limit")
	if err != nil {
		return chat.ListThreadsOptions{}, err
	}
	opts.Limit = limit
	opts.Search = query.Get("search")
	opts.Cursor = query.Get("cursor")
	return opts, nil
}
