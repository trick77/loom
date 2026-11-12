package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/ideas"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/ship/kit/httpx"
)

// ideasOn writes a 503 when the idea store is not wired (tests that build
// Deps without one).
func (s *server) ideasOn(w http.ResponseWriter) bool {
	if s.ideas == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "ideas are not configured")
		return false
	}
	return true
}

// userToday is the user's civil date: follow-up dates are days in the
// user's zone (the request's, kept on the user row by rememberTimezone).
func (s *server) userToday(r *http.Request, user auth.User) time.Time {
	loc, err := schedule.LoadLocation(s.rememberTimezone(r.Context(), user, r))
	if err != nil {
		loc = time.UTC
	}
	y, m, d := time.Now().In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// writeIdeaError maps the ideas package's errors to responses: a foreign or
// unknown id is a 404, never a 403.
func writeIdeaError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *ideas.ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, http.StatusBadRequest, validation.Msg)
	case errors.Is(err, ideas.ErrNotFound):
		writeNotFound(w)
	default:
		serverError(w, r, err, "idea store failed")
	}
}

func (s *server) handleListIdeas(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = ideas.StateOpen
	}
	items, err := s.ideas.List(r.Context(), user.ID, status, s.userToday(r, user))
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) handleCountIdeas(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	open, done, err := s.ideas.Count(r.Context(), user.ID)
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"open": open, "done": done})
}

func (s *server) handleGetIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	idea, err := s.ideas.Get(r.Context(), user.ID, r.PathValue("ideaID"))
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, idea)
}

type createIdeaRequest struct {
	Title     string             `json:"title"`
	Reason    string             `json:"reason"`
	Excerpt   string             `json:"excerpt"`
	ThreadID  string             `json:"threadId"`
	MessageID string             `json:"messageId"`
	DueOn     ideas.OptionalDate `json:"dueOn"`
	RemindOn  ideas.OptionalDate `json:"remindOn"`
}

func (s *server) handleCreateIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	var req createIdeaRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	idea, err := s.ideas.Create(r.Context(), user.ID, ideas.StateOpen, ideas.Input{
		Title:     req.Title,
		Reason:    req.Reason,
		Excerpt:   req.Excerpt,
		ThreadID:  req.ThreadID,
		MessageID: req.MessageID,
		DueOn:     req.DueOn,
		RemindOn:  req.RemindOn,
	}, s.userToday(r, user))
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, idea)
}

type patchIdeaRequest struct {
	Title           *string            `json:"title"`
	Reason          *string            `json:"reason"`
	Excerpt         *string            `json:"excerpt"`
	DueOn           ideas.OptionalDate `json:"dueOn"`
	RemindOn        ideas.OptionalDate `json:"remindOn"`
	Done            *bool              `json:"done"`
	RemindDismissed *bool              `json:"remindDismissed"`
}

func (s *server) handlePatchIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	var req patchIdeaRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	idea, err := s.ideas.Update(r.Context(), user.ID, r.PathValue("ideaID"), ideas.Patch{
		Title:           req.Title,
		Reason:          req.Reason,
		Excerpt:         req.Excerpt,
		DueOn:           req.DueOn,
		RemindOn:        req.RemindOn,
		Done:            req.Done,
		RemindDismissed: req.RemindDismissed,
	}, s.userToday(r, user))
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, idea)
}

// handleDeleteIdea deletes softly (restorable for 30 days); a proposal is
// discarded for good (its card's "Dismiss").
func (s *server) handleDeleteIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	if err := s.ideas.Delete(r.Context(), user.ID, r.PathValue("ideaID")); err != nil {
		writeIdeaError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleRestoreIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	idea, err := s.ideas.Restore(r.Context(), user.ID, r.PathValue("ideaID"))
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, idea)
}

// handleConfirmIdea turns a proposal from the idea tools into an idea: the
// user's click on its card, an authenticated request no tool can make.
func (s *server) handleConfirmIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	idea, err := s.ideas.Confirm(r.Context(), user.ID, r.PathValue("ideaID"))
	if err != nil {
		writeIdeaError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, idea)
}
