package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/usage"
	"github.com/trick77/ship/kit/httpx"
)

// stopProjectThreadStreams terminates any turn still generating on a thread of the
// project and waits for each to unwind. Both the active and the archived listing
// are swept: a thread archived while its turn ran still streams. Best effort — a
// listing failure must not block the delete, it only leaves a stream running as it
// did before. It returns the thread ids it could list.
func (s *server) stopProjectThreadStreams(ctx context.Context, userID, projectID string) []string {
	var all []string
	for _, archived := range []bool{false, true} {
		ids, err := s.thread.ListThreadIDs(ctx, userID, chat.ListThreadsOptions{ProjectID: &projectID, Archived: archived})
		if err != nil {
			slog.Warn("list project threads for stream stop failed", "project_id", projectID, "archived", archived, "err", err)
			continue
		}
		for _, threadID := range ids {
			if slices.Contains(all, threadID) {
				continue
			}
			s.stopThreadWork(userID, threadID)
			all = append(all, threadID)
		}
	}
	return all
}

// detachProjectArtifactsInUse is detachArtifactsInUse for every thread the
// project's delete cascades. Runs after the project's documents are deleted,
// so only documents outliving the project match.
func (s *server) detachProjectArtifactsInUse(ctx context.Context, userID string, threadIDs []string) (map[string]struct{}, error) {
	keep := map[string]struct{}{}
	for _, threadID := range threadIDs {
		ids, err := s.detachArtifactsInUse(ctx, userID, threadID)
		if err != nil {
			return nil, err
		}
		for id := range ids {
			keep[id] = struct{}{}
		}
	}
	return keep, nil
}

func (s *server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	archived, err := parseOptionalBool(r, "archived")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid archived query parameter")
		return
	}
	projects, err := s.thread.ListProjects(r.Context(), user.ID, archived)
	if err != nil {
		serverError(w, r, err, "list projects failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projects)
}

func (s *server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body createProjectRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	project, err := s.thread.CreateProject(r.Context(), user.ID, chat.CreateProjectInput{
		Name:        body.Name,
		Description: body.Description,
	})
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	usage.Record(s.usage, "project_created", func() error { return s.usage.IncProjectCreated(r.Context(), user.ID) })
	httpx.WriteJSON(w, http.StatusCreated, project)
}

func (s *server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	var body updateProjectRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	project, found, err := s.thread.UpdateProject(r.Context(), user.ID, r.PathValue("projectID"), body.toInput())
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	if !found {
		writeNotFound(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project)
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
		writeNotFound(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project)
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
		writeNotFound(w)
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
	threadIDs := s.stopProjectThreadStreams(r.Context(), user.ID, projectID)
	// Must run before DeleteProject: the projects FK cascade would otherwise drop
	// the chunk rows and orphan their vec0 embeddings (a vtab unreachable by
	// cascade). And before the in-use query below, so it sees only documents
	// that outlive the project.
	if s.documents != nil {
		if err := s.documents.DeleteProjectData(r.Context(), user.ID, projectID); err != nil {
			serverError(w, r, err, "delete project knowledge failed")
			return
		}
	}
	// Snapshot before the delete: the FK cascade destroys the artifact rows.
	artifacts, err := s.artifactsForProjectCleanup(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "list project artifacts failed")
		return
	}
	keep, err := s.detachProjectArtifactsInUse(r.Context(), user.ID, threadIDs)
	if err != nil {
		serverError(w, r, err, "detach in-use project artifacts failed")
		return
	}
	found, err := s.thread.DeleteProject(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "delete project failed")
		return
	}
	if !found {
		writeNotFound(w)
		return
	}
	s.cleanupArtifactFiles(user.ID, artifacts, keep)
	w.WriteHeader(http.StatusNoContent)
}
