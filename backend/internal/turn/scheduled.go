package turn

import (
	"regexp"
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
// The run sees the user's standing instructions and every server's guidance,
// both written for a live chat, so it says which parts still apply.
const scheduledRunPrompt = "You are running a scheduled task on your own: nobody is waiting in the chat and nobody can answer a question before the next run. Research with the tools this task needs, then answer with what is NEW since earlier runs, naming each new item with its source. When nothing is new, say so in one sentence. Never ask the user anything. These rules of the task come before any standing instruction or tool guidance written for a live chat; follow those only where they fit an unattended run, such as the answer's language or tone."

// allowTools keeps the tools a scheduled run may use: every MCP tool the
// user may use (offerTools already left out other accounts' servers), never a
// built-in, so no memory, file, sandbox or schedule tool. It records them for
// dispatch and returns no built-ins to offer. Any other turn keeps all of them.
func (t *Run) allowTools(tools []llm.Tool, builtIns map[string]*toolSpec) ([]llm.Tool, map[string]*toolSpec) {
	if t.scheduled == nil {
		return tools, builtIns
	}
	tools = slices.DeleteFunc(tools, func(tool llm.Tool) bool {
		return builtIns[tool.Function.Name] != nil
	})
	t.scheduledTools = make(map[string]bool, len(tools))
	for _, tool := range tools {
		t.scheduledTools[tool.Function.Name] = true
	}
	return tools, map[string]*toolSpec{}
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

// withFinalContext is history with a scheduled run's user memory and
// directives added to its system prompt, for a call that offers no tools.
// Any other turn's history comes back as it is.
func (t *Run) withFinalContext(history []llm.Message) []llm.Message {
	if t.plan.finalContext == "" {
		return history
	}
	out := slices.Clone(history)
	if len(out) > 0 && out[0].Role == "system" {
		out[0].Content = joinNonEmptyBlocks(out[0].Content, t.plan.finalContext)
		return out
	}
	return append([]llm.Message{{Role: "system", Content: t.plan.finalContext}}, out...)
}

// markdownImage is an inline or reference markdown image: ![alt](src) or
// ![alt][ref].
var markdownImage = regexp.MustCompile(`!\[([^\]]*)\](\([^)]*\)|\[[^\]]*\])`)

// dropImages keeps each markdown image's alt text only. Removing one can
// close up a new one (![![x](y)](z)), so it repeats until none is left.
func dropImages(s string) string {
	for markdownImage.MatchString(s) {
		s = markdownImage.ReplaceAllString(s, "$1")
	}
	return s
}

// dropAnswerImages strips the images from a scheduled run's answer. Opening
// the thread would load them, so an image an injected page slipped into the
// answer could carry what the run knows out in its URL, without a click.
// Links stay: they need one.
func dropAnswerImages(out LoopResult) LoopResult {
	out.Content = dropImages(out.Content)
	for i := range out.Blocks {
		if out.Blocks[i].Type == "text" {
			out.Blocks[i].Content = dropImages(out.Blocks[i].Content)
		}
	}
	return out
}
