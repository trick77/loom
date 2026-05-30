package httpapi

import (
	"net/http"

	"github.com/trick77/spark/internal/chat"
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
		writeJSONError(w, http.StatusInternalServerError, "list threads failed")
		return
	}
	writeJSON(w, threads)
}

func (s *server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	var body createThreadRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	thread, err := s.chat.CreateThread(r.Context(), user.ID, chat.CreateThreadInput{
		ProjectID: body.ProjectID,
		Title:     body.Title,
	})
	if err != nil {
		writeChatStoreError(w, err, http.StatusNotFound, "project not found")
		return
	}
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
		writeJSONError(w, http.StatusInternalServerError, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	messages, found, err := s.chat.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list messages failed")
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
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	thread, found, err := s.chat.UpdateThread(r.Context(), user.ID, r.PathValue("threadID"), chat.UpdateThreadInput{Title: body.Title})
	if err != nil {
		writeChatStoreError(w, err, http.StatusBadRequest, "thread title is required")
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
		writeJSONError(w, http.StatusInternalServerError, "update thread failed")
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
		writeJSONError(w, http.StatusInternalServerError, "update thread failed")
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
	found, err := s.chat.DeleteThread(r.Context(), user.ID, r.PathValue("threadID"))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	return opts, nil
}
