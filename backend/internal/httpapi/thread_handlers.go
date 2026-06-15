package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/trick77/lume/internal/chat"
)

func (s *server) handleListThreads(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	opts, err := listThreadsOptionsFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	threads, err := s.chat.ListThreads(r.Context(), user.ID, opts)
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

func (s *server) handleListThreadIDs(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	opts, err := listThreadsOptionsFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids, err := s.chat.ListThreadIDs(r.Context(), user.ID, opts)
	if err != nil {
		serverError(w, r, err, "list thread ids failed")
		return
	}
	writeJSON(w, ids)
}

func (s *server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	var body createThreadRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	thread, err := s.chat.CreateThread(r.Context(), user.ID, chat.CreateThreadInput{
		ProjectID: body.ProjectID,
		Title:     body.Title,
	})
	if err != nil {
		writeMappedChatStoreError(w, r, err, map[string]int{
			"project not found":        http.StatusNotFound,
			"thread title is too long": http.StatusBadRequest,
		})
		return
	}
	s.recordUsage("chat_created", func() error { return s.usage.IncChatCreated(r.Context(), user.ID) })
	writeJSONStatus(w, http.StatusCreated, thread)
}

func (s *server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	thread, found, err := s.chat.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	messages, found, err := s.chat.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "list messages failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, getThreadResponse{Thread: thread, Messages: messages})
}

func (s *server) handleUpdateThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	var body updateThreadRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	thread, found, err := s.chat.UpdateThread(r.Context(), user.ID, r.PathValue("threadID"), body.chatInput())
	if err != nil {
		writeMappedChatStoreError(w, r, err, map[string]int{
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
	if !ok || !requireChat(w, s) {
		return
	}
	thread, found, err := s.chat.SetThreadStarred(r.Context(), user.ID, r.PathValue("threadID"), starred)
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
	if !ok || !requireChat(w, s) {
		return
	}
	found, err := s.chat.SetThreadArchived(r.Context(), user.ID, r.PathValue("threadID"), archived)
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
	if !ok || !requireChat(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	artifacts, err := s.artifactsForThreadCleanup(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "list thread artifacts failed")
		return
	}
	if s.documents != nil {
		if err := s.documents.DeleteThreadData(r.Context(), user.ID, threadID); err != nil {
			serverError(w, r, err, "delete thread knowledge failed")
			return
		}
	}
	found, err := s.chat.DeleteThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "delete thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	s.cleanupArtifactFiles(user.ID, artifacts)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleBulkDeleteThreads(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
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
		artifacts, err := s.artifactsForThreadCleanup(r.Context(), user.ID, threadID)
		if err != nil {
			slog.Warn("bulk delete: skip thread, artifact cleanup failed", "thread_id", threadID, "err", err)
			continue
		}
		if s.documents != nil {
			if err := s.documents.DeleteThreadData(r.Context(), user.ID, threadID); err != nil {
				continue
			}
		}
		found, err := s.chat.DeleteThread(r.Context(), user.ID, threadID)
		if err != nil {
			slog.Warn("bulk delete: skip thread, delete failed", "thread_id", threadID, "err", err)
			continue
		}
		if !found {
			continue
		}
		s.cleanupArtifactFiles(user.ID, artifacts)
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
