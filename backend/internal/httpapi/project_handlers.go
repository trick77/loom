package httpapi

import (
	"net/http"

	"github.com/trick77/slopr/internal/chat"
)

func (s *server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	archived, err := parseOptionalBool(r, "archived")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid archived query parameter")
		return
	}
	projects, err := s.chat.ListProjects(r.Context(), user.ID, archived)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list projects failed")
		return
	}
	writeJSON(w, projects)
}

func (s *server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	var body createProjectRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, err := s.chat.CreateProject(r.Context(), user.ID, chat.CreateProjectInput{
		Name:        body.Name,
		Description: body.Description,
	})
	if err != nil {
		writeChatStoreError(w, err, http.StatusBadRequest, "project name is required", "project name is too long", "project description is too long")
		return
	}
	s.recordUsage("project_created", func() error { return s.usage.IncProjectCreated(r.Context(), user.ID) })
	writeJSONStatus(w, http.StatusCreated, project)
}

func (s *server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	var body updateProjectRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, found, err := s.chat.UpdateProject(r.Context(), user.ID, r.PathValue("projectID"), body.chatInput())
	if err != nil {
		writeChatStoreError(w, err, http.StatusBadRequest, "project name is required", "project name is too long", "project description is too long")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, project)
}

func (s *server) handleArchiveProject(w http.ResponseWriter, r *http.Request) {
	s.handleSetProjectArchived(w, r, true)
}

func (s *server) handleUnarchiveProject(w http.ResponseWriter, r *http.Request) {
	s.handleSetProjectArchived(w, r, false)
}

func (s *server) handleSetProjectArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	found, err := s.chat.SetProjectArchived(r.Context(), user.ID, r.PathValue("projectID"), archived)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "update project failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	projectID := r.PathValue("projectID")
	artifacts, err := s.artifactsForProjectCleanup(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list project artifacts failed")
		return
	}
	found, err := s.chat.DeleteProject(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete project failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	s.cleanupArtifactFiles(user.ID, artifacts)
	w.WriteHeader(http.StatusNoContent)
}
