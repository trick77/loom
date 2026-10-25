package turn

import (
	"slices"
	"time"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// ScheduledRun makes a persisted turn a schedule's headless run: a fixed tool
// set, a token budget and a soft deadline, the schedule's memory in the prompt,
// and every web tool outcome reported to Observe.
type ScheduledRun struct {
	// Tools are the only tools the run is offered (see ScheduledRunTools).
	Tools []string
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

// ScheduledRunTools is the research set a scheduled run gets: web search,
// fetch and the headless browser. Never the schedule tools.
func ScheduledRunTools() []string {
	return []string{tavilySearchExposedName, fetchToolName, obscuraNavigateToolName, obscuraSnapshotToolName}
}

// allowTools keeps the tools a scheduled run may use; any other turn keeps
// all of them.
func (t *Run) allowTools(tools []llm.Tool) []llm.Tool {
	if t.scheduled == nil {
		return tools
	}
	return slices.DeleteFunc(tools, func(tool llm.Tool) bool {
		return !slices.Contains(t.scheduled.Tools, tool.Function.Name)
	})
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
