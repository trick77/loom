// Package toolactions keeps the calls to acting tools of confirm servers
// (mcp.json "confirm": true) that a turn showed as a card instead of making:
// only the user's click runs one, with the stored arguments.
package toolactions

import (
	"encoding/json"
	"time"

	"github.com/trick77/loom/internal/mcp"
)

// States. A proposal is claimed (running) by the click that runs it, so a
// second click never runs it twice, and settles done or failed.
const (
	StateProposed  = "proposed"
	StateRunning   = "running"
	StateDone      = "done"
	StateFailed    = "failed"
	StateDismissed = "dismissed"
)

// Action is one proposed tool call and what became of it.
//
// Table: tool_actions
type Action struct {
	ID       string
	UserID   string
	ThreadID string
	// Tool is the exposed MCP tool name (server__tool).
	Tool string
	// Arguments are the model's arguments, exactly what the click sends.
	Arguments json.RawMessage
	State     string
	Result    *Result
	CreatedAt time.Time
	DecidedAt *time.Time
}

// Result is what a call returned, for its card: the server's text, a status
// per element of its array argument and one link to open. Stored as JSON in
// tool_actions.result.
//
// Table: -
type Result struct {
	Text  string           `json:"text"`
	Items []mcp.ActionItem `json:"items,omitempty"`
	Link  *mcp.ActionLink  `json:"link,omitempty"`
}
