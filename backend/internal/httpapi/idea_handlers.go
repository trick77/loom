package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/ideas"
	"github.com/trick77/loom/internal/llm"
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

// ideaDraftTimeout bounds the title proposal: the capture form opens with
// an empty title and fills it when this answers.
const ideaDraftTimeout = 15 * time.Second

type draftIdeaRequest struct {
	Excerpt string `json:"excerpt"`
	Reason  string `json:"reason"`
}

// handleDraftIdea proposes a title for an idea being captured. No model, a
// failed call or an unusable reply all answer an empty title: the user types
// one. The reply never reaches a tool or a store; it is capped and cleaned
// like any title.
func (s *server) handleDraftIdea(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.ideasOn(w) {
		return
	}
	var req draftIdeaRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	excerpt := ideas.CleanText(req.Excerpt, ideas.MaxExcerptRunes)
	reason := ideas.CleanLine(req.Reason, ideas.MaxReasonRunes)
	out := draftIdeaResponse{}
	// One proposal per user at a time: a second request while one runs gets
	// nothing rather than another model call.
	release, free := s.inflight.tryAcquire("idea-draft:" + user.ID)
	if free {
		defer release()
	}
	if free && s.ideaLLM != nil && (excerpt != "" || reason != "") {
		ctx, cancel := context.WithTimeout(r.Context(), ideaDraftTimeout)
		defer cancel()
		today := s.userToday(r, user)
		inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "idea_draft", Round: 1}
		drafted, err := s.ideaLLM.DraftIdea(llm.WithInferenceMetadata(ctx, inference), excerpt, reason, ideas.FormatDate(today), user.ResponseLanguageName())
		if err != nil {
			slog.Warn("draft idea failed", "user_id", user.ID, "err", err)
		} else {
			out.Title = ideas.CleanLine(drafted.Title, ideas.MaxTitleRunes)
			// A date only from a call without the passage: outside text
			// that names one must not set a due date (and through it a
			// push), whatever the prompt says. The UI asks for the title
			// with the passage and for the date with the reason alone.
			if excerpt == "" {
				out.DueOn = draftedDueOn(drafted.DueOn, today)
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type draftIdeaResponse struct {
	Title string  `json:"title"`
	DueOn *string `json:"dueOn"`
}

// draftedMaxAhead is the furthest a drafted due date may lie.
const draftedMaxAhead = 3

// draftedDueOn keeps a model's due date only when it is a real date, not
// before today and within draftedMaxAhead years; the follow-up is the
// server's to compute (ideas.DefaultRemindOn), never the model's.
func draftedDueOn(raw string, today time.Time) *string {
	d, err := ideas.ParseDate(raw)
	if err != nil || d.Before(today) || d.After(today.AddDate(draftedMaxAhead, 0, 0)) {
		return nil
	}
	s := ideas.FormatDate(d)
	return &s
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
	// Done, hidden or re-dated: its follow-up notice in the bell is settled
	// (a moved follow-up sends a new one on its day).
	settled := (req.Done != nil && *req.Done) || (req.RemindDismissed != nil && *req.RemindDismissed) || req.RemindOn.Set || req.DueOn.Set
	if settled {
		s.settleIdeaNotices(r.Context(), user.ID, idea.ID)
	}
	httpx.WriteJSON(w, http.StatusOK, idea)
}

// settleIdeaNotices marks an idea's follow-up notices read, best effort:
// the idea is done, deleted or re-dated, the bell has nothing left to say.
func (s *server) settleIdeaNotices(ctx context.Context, userID, ideaID string) {
	if s.schedules == nil {
		return
	}
	if err := s.schedules.MarkIdeaRead(ctx, userID, ideaID); err != nil {
		slog.Warn("mark idea notifications read failed", "idea_id", ideaID, "err", err)
	}
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
	s.settleIdeaNotices(r.Context(), user.ID, r.PathValue("ideaID"))
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
