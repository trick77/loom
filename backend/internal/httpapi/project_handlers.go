package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/chat"
)

// stopProjectThreadStreams terminates any turn still generating on a thread of the
// project and waits for each to unwind. Both the active and the archived listing
// are swept: a thread archived while its turn ran still streams. Best effort — a
// listing failure must not block the delete, it only leaves a stream running as it
// did before.
func (s *server) stopProjectThreadStreams(ctx context.Context, userID, projectID string) {
	for _, archived := range []bool{false, true} {
		ids, err := s.thread.ListThreadIDs(ctx, userID, chat.ListThreadsOptions{ProjectID: &projectID, Archived: archived})
		if err != nil {
			slog.Warn("list project threads for stream stop failed", "project_id", projectID, "archived", archived, "err", err)
			continue
		}
		for _, threadID := range ids {
			s.activeStreams.stopAndWait(userID, threadID, errStreamThreadDeleted, threadDeleteStopTimeout)
		}
	}
}

func (s *server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	archived, err := parseOptionalBool(r, "archived")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid archived query parameter")
		return
	}
	projects, err := s.thread.ListProjects(r.Context(), user.ID, archived)
	if err != nil {
		serverError(w, r, err, "list projects failed")
		return
	}
	writeJSON(w, projects)
}

func (s *server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body createProjectRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, err := s.thread.CreateProject(r.Context(), user.ID, chat.CreateProjectInput{
		Name:        body.Name,
		Description: body.Description,
	})
	if err != nil {
		writeThreadStoreError(w, r, err, http.StatusBadRequest, "project name is required", "project name is too long", "project description is too long")
		return
	}
	s.recordUsage("project_created", func() error { return s.usage.IncProjectCreated(r.Context(), user.ID) })
	writeJSONStatus(w, http.StatusCreated, project)
}

func (s *server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body updateProjectRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, found, err := s.thread.UpdateProject(r.Context(), user.ID, r.PathValue("projectID"), body.toInput())
	if err != nil {
		writeThreadStoreError(w, r, err, http.StatusBadRequest, "project name is required", "project name is too long", "project description is too long")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, project)
}

func (s *server) handleStarProject(w http.ResponseWriter, r *http.Request) {
	s.handleSetProjectStarred(w, r, true)
}

func (s *server) handleUnstarProject(w http.ResponseWriter, r *http.Request) {
	s.handleSetProjectStarred(w, r, false)
}

func (s *server) handleSetProjectStarred(w http.ResponseWriter, r *http.Request, starred bool) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	project, found, err := s.thread.SetProjectStarred(r.Context(), user.ID, r.PathValue("projectID"), starred)
	if err != nil {
		serverError(w, r, err, "update project failed")
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
	if !ok || !requireThreadStore(w, s) {
		return
	}
	found, err := s.thread.SetProjectArchived(r.Context(), user.ID, r.PathValue("projectID"), archived)
	if err != nil {
		serverError(w, r, err, "update project failed")
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
	if !ok || !requireThreadStore(w, s) {
		return
	}
	projectID := r.PathValue("projectID")
	// Threads cascade with the project, so a turn generating on one of them has to
	// be terminated first for the same reasons as in handleDeleteThread: it would
	// otherwise keep calling the model, then fail its writes on the vanished rows
	// and leave artifact files the cleanup below cannot see.
	s.stopProjectThreadStreams(r.Context(), user.ID, projectID)
	artifacts, err := s.artifactsForProjectCleanup(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "list project artifacts failed")
		return
	}
	// Must run before DeleteProject: the projects FK cascade would otherwise drop
	// the chunk rows and orphan their vec0 embeddings (a vtab unreachable by
	// cascade).
	if s.documents != nil {
		if err := s.documents.DeleteProjectData(r.Context(), user.ID, projectID); err != nil {
			serverError(w, r, err, "delete project knowledge failed")
			return
		}
	}
	found, err := s.thread.DeleteProject(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "delete project failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	s.cleanupArtifactFiles(user.ID, artifacts)
	w.WriteHeader(http.StatusNoContent)
}
