package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/inference"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/toolactions"
	"github.com/trick77/loom/internal/turn"
	"github.com/trick77/ship/kit/httpx"
)

// actionCallTimeout bounds the call a click runs. It outlives the request:
// a closed tab still settles the card.
const actionCallTimeout = 2 * time.Minute

// maxActionResultText bounds the server's text a card keeps.
const maxActionResultText = 4000

// handleRunToolAction runs a proposed call: the user's click on its card, an
// authenticated request no tool can make. It runs once (Claim), with the
// stored arguments, as the user, and settles the card with the result. An
// action no longer waiting answers 409 with its card as it stands.
func (s *server) handleRunToolAction(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.toolActionsOn(w) {
		return
	}
	action, err := s.actions.Claim(r.Context(), user.ID, r.PathValue("actionID"))
	if !s.writeActionConflict(w, r, action, err) {
		return
	}
	state, result := s.runToolAction(context.WithoutCancel(r.Context()), user, action)
	// The call happened: settling it is worth a few tries (a busy database),
	// or the card would only ever say its outcome is unknown.
	for attempt := 0; ; attempt++ {
		action, err = s.actions.Finish(context.WithoutCancel(r.Context()), user.ID, action.ID, state, result)
		if err == nil || attempt == finishAttempts-1 || errors.Is(err, toolactions.ErrNotFound) || errors.Is(err, toolactions.ErrNotPending) {
			break
		}
		time.Sleep(finishRetryDelay)
	}
	if err != nil {
		serverError(w, r, err, "tool action store failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.actionCard(action))
}

// finishAttempts and finishRetryDelay bound how hard a click tries to
// store the outcome of a call that already ran.
const (
	finishAttempts   = 3
	finishRetryDelay = 300 * time.Millisecond
)

// handleGetToolAction is an action's card as it stands: a card that shows a
// call running (another tab clicked) asks until it settles.
func (s *server) handleGetToolAction(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.toolActionsOn(w) {
		return
	}
	id := r.PathValue("actionID")
	byID, err := s.actions.GetMany(r.Context(), user.ID, []string{id})
	if err != nil {
		serverError(w, r, err, "tool action store failed")
		return
	}
	action, found := byID[id]
	if !found {
		writeNotFound(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.actionCard(action))
}

// handleDismissToolAction discards a proposal: its card's "Dismiss".
func (s *server) handleDismissToolAction(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.toolActionsOn(w) {
		return
	}
	action, err := s.actions.Dismiss(r.Context(), user.ID, r.PathValue("actionID"))
	if !s.writeActionConflict(w, r, action, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.actionCard(action))
}

func (s *server) toolActionsOn(w http.ResponseWriter) bool {
	if s.actions == nil || s.mcp == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tool actions are not configured")
		return false
	}
	return true
}

// writeActionConflict answers a store error and reports whether the call
// may go on: 404 for an action the user does not have, 409 with the card
// for one no longer waiting.
func (s *server) writeActionConflict(w http.ResponseWriter, r *http.Request, action toolactions.Action, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, toolactions.ErrNotFound):
		writeNotFound(w)
	case errors.Is(err, toolactions.ErrNotPending):
		httpx.WriteJSON(w, http.StatusConflict, s.actionCard(action))
	default:
		serverError(w, r, err, "tool action store failed")
	}
	return false
}

// runToolAction makes the call as the user. A tool gone from the config or
// no longer the user's fails without a call; the server's own error text
// (isError) is the card's, any other failure is only logged.
func (s *server) runToolAction(ctx context.Context, user auth.User, action toolactions.Action) (string, toolactions.Result) {
	principal := turn.MCPPrincipal(user)
	if !s.mcp.HasTool(action.Tool) || !s.mcp.ToolAllowed(principal, action.Tool) {
		return toolactions.StateFailed, toolactions.Result{}
	}
	var arguments map[string]any
	if err := json.Unmarshal(action.Arguments, &arguments); err != nil {
		return toolactions.StateFailed, toolactions.Result{}
	}
	ctx, cancel := context.WithTimeout(ctx, actionCallTimeout)
	defer cancel()
	ctx = mcp.WithPrincipal(inference.WithAttribution(ctx, user.ID, user.Username, action.ThreadID), principal)
	res, err := s.mcp.CallToolResult(ctx, action.Tool, arguments)
	if err != nil {
		var toolErr *mcp.ToolError
		if errors.As(err, &toolErr) {
			return toolactions.StateFailed, toolactions.Result{Text: capText(toolErr.Text)}
		}
		slog.Warn("tool action failed", "tool", action.Tool, "err", redactErr(err))
		return toolactions.StateFailed, toolactions.Result{}
	}
	return toolactions.StateDone, toolactions.Result{Text: capText(res.Text), Items: res.Items, Link: res.Link}
}

func capText(s string) string {
	if r := []rune(s); len(r) > maxActionResultText {
		return string(r[:maxActionResultText]) + "…"
	}
	return s
}

// actionCard is the card of an action as it stands now.
func (s *server) actionCard(a toolactions.Action) turn.ActionCard {
	server, _, _ := mcp.SplitExposedToolName(a.Tool)
	return withAction(turn.ActionCard{Server: server, Title: s.mcp.ToolTitle(a.Tool)}, a)
}

// staleRunning is how long a claimed call may run before its card says it
// was interrupted (a restart mid-call never settles it).
const staleRunning = actionCallTimeout + time.Minute

// withAction overlays an action's id, arguments, state and result on a card;
// a proposal past its retention is gone, a claim that never settled has an
// unknown outcome (the call may have happened).
func withAction(card turn.ActionCard, a toolactions.Action) turn.ActionCard {
	card.ActionID, card.Arguments, card.State, card.Result = a.ID, a.Arguments, a.State, a.Result
	switch {
	case a.Expired():
		card.State = turn.ActionCardGone
	case a.State == toolactions.StateRunning && a.DecidedAt != nil && time.Since(*a.DecidedAt) > staleRunning:
		card.State = turn.ActionCardUnknown
	}
	return card
}
