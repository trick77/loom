package turn

import (
	"slices"
	"time"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// ScheduledRun makes a persisted turn a schedule's headless run: the user's
// MCP tools only, a token budget and a soft deadline, the schedule's memory in
// the prompt, and every web tool outcome reported to Observe.
type ScheduledRun struct {
	// TokenBudget caps the tokens of every model call of the run; past it the
	// loop takes the forced final answer. Zero means no cap.
	TokenBudget int
	// SoftDeadline is when the loop stops offering tools and answers; zero
	// means none.
	SoftDeadline time.Time
	// Memory is the schedule's memory block for the system prompt.
	Memory string
	// Observe receives each web tool call's outcome; nil drops them.
	Observe func(ToolObservation)
	// Now is the clock the deadline is read against; nil is time.Now.
	Now func() time.Time
}

// ToolObservation is what one web tool call delivered, for a schedule's
// learned sources.
type ToolObservation struct {
	// Tool is "fetch", "browser" or "search".
	Tool  string
	URL   string
	Query string
	OK    bool
	// FellBack is a fetch that failed while the browser fallback delivered.
	FellBack bool
	// ResultURLs are the pages a search returned.
	ResultURLs []string
}

// Limits a scheduled run can reach (LoopResult.LimitHit).
const (
	LimitTokens = "tokens"
	LimitTime   = "time"
)

// scheduledRunPrompt tells the model it runs unattended.
const scheduledRunPrompt = "You are running a scheduled task on your own: nobody is waiting in the chat and nobody can answer a question before the next run. Research with the tools, then answer with what is NEW since earlier runs, naming each new item with its source. When nothing is new, say so in one sentence. Never ask the user anything."

// allowTools keeps the tools a scheduled run may use: every MCP tool the
// user may use (offerTools already left out other accounts' servers), never a
// built-in, so no memory, file, sandbox or schedule tool. It records them for
// dispatch and empties builtIns. Any other turn keeps all of them.
func (t *Run) allowTools(tools []llm.Tool, builtIns map[string]*toolSpec) []llm.Tool {
	if t.scheduled == nil {
		return tools
	}
	tools = slices.DeleteFunc(tools, func(tool llm.Tool) bool {
		return builtIns[tool.Function.Name] != nil
	})
	clear(builtIns)
	t.scheduledTools = make(map[string]bool, len(tools))
	for _, tool := range tools {
		t.scheduledTools[tool.Function.Name] = true
	}
	return tools
}

// scheduledDenies reports whether a scheduled run refuses a tool call: one
// to a tool it was not offered, whatever the model names.
func (t *Run) scheduledDenies(name string) bool {
	return t.scheduled != nil && !t.scheduledTools[name]
}

// overLimit reports which limit the scheduled run reached, or "".
func (t *Run) overLimit() string {
	sr := t.scheduled
	if sr == nil {
		return ""
	}
	if sr.TokenBudget > 0 && t.acc != nil && t.acc.Total().TotalTokens >= sr.TokenBudget {
		return LimitTokens
	}
	now := time.Now
	if sr.Now != nil {
		now = sr.Now
	}
	if !sr.SoftDeadline.IsZero() && !now().Before(sr.SoftDeadline) {
		return LimitTime
	}
	return ""
}

// observeToolCall reports a finished web tool call to the scheduled run.
func (t *Run) observeToolCall(name string, arguments map[string]any, sources []mcp.Source, ok, fellBack bool) {
	if t.scheduled == nil || t.scheduled.Observe == nil {
		return
	}
	o := ToolObservation{OK: ok, FellBack: fellBack}
	switch name {
	case fetchToolName:
		o.Tool, o.URL = "fetch", argURL(arguments)
	case obscuraNavigateToolName:
		o.Tool, o.URL = "browser", argURL(arguments)
	case tavilySearchExposedName:
		o.Tool, o.Query = "search", stringArg(arguments, "query")
		for _, s := range sources {
			o.ResultURLs = append(o.ResultURLs, s.URL)
		}
	default:
		return
	}
	t.scheduled.Observe(o)
}
