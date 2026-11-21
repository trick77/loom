package turn

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/toolactions"
)

// An acting tool of a confirm server (mcp.json "confirm": true) never runs
// in a turn: the turn records the call and shows it as a card with its
// arguments, and only the user's click (an authenticated request from the
// UI) runs it, with exactly those arguments. So the call is no action of the
// turn's: outside content in the turn may propose it but never make it, and
// the outside-content lock (Run.refusesAction) does not apply. A scheduled
// run never gets such a tool (it acts).

// ToolActions records a proposed call for the user's click.
type ToolActions interface {
	Propose(ctx context.Context, userID, threadID, tool string, arguments json.RawMessage) (toolactions.Action, error)
}

// ActionCard is a proposed call's card in the thread: the server, the tool's
// title and the arguments the click sends. State and Result are the action's
// at read time, overlaid on every load; "gone" is a proposal past its
// retention or no longer stored.
type ActionCard struct {
	ActionID  string              `json:"actionId"`
	State     string              `json:"state"`
	Server    string              `json:"server"`
	Title     string              `json:"title"`
	Arguments json.RawMessage     `json:"arguments"`
	Result    *toolactions.Result `json:"result,omitempty"`
}

// ActionCardGone is the state of a card whose action can no longer run;
// ActionCardUnknown one whose call started but never settled (a restart, a
// store failure after the call): it may have happened, so no retry advice.
const (
	ActionCardGone    = "gone"
	ActionCardUnknown = "unknown"
)

// actionProposed is what the model reads for a proposed call.
const actionProposed = "Not run: shown to the user as a card with exactly these arguments. It runs only when they click Run on it, after your answer, and you will not see the result. Tell them it is waiting for their click on the card; do not say it is done."

// confirms reports whether a call is proposed instead of made.
func (s *Engine) confirms(name string) bool {
	return s.actions != nil && s.mcp != nil && s.mcp.Confirms(name)
}

// proposeAction records call for the user's click and returns what the
// model reads and the card to show; no card when it could not be recorded.
func (t *Run) proposeAction(ctx context.Context, call llm.ToolCall) (string, *ActionCard) {
	arguments := json.RawMessage(call.Function.Arguments)
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	var object map[string]any
	if json.Unmarshal(arguments, &object) != nil || object == nil {
		return capToolOutput("tool failed: invalid arguments: not a JSON object"), nil
	}
	action, err := t.e.actions.Propose(ctx, t.user.ID, t.thread.ID, call.Function.Name, arguments)
	if err != nil {
		slog.Warn("tool action not recorded", "tool", call.Function.Name, "err", err)
		return "tool failed: could not show the card; ask the user to try again later", nil
	}
	return actionProposed, &ActionCard{
		ActionID:  action.ID,
		State:     action.State,
		Server:    actionServer(call.Function.Name),
		Title:     t.e.mcp.ToolTitle(call.Function.Name),
		Arguments: action.Arguments,
	}
}

// actionServer is the server part of an exposed tool name.
func actionServer(name string) string {
	server, _, _ := mcp.SplitExposedToolName(name)
	return server
}
