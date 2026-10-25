package turn

import (
	"regexp"
	"slices"
	"strings"
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
// The run sees every server's guidance and, when it writes the answer, the
// user's standing instructions, both written for a live chat, so it says
// which parts still apply.
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

// dropImages leaves no markdown image outside a fenced code block, where
// none renders. A plain image keeps its alt text; any other image marker
// (balanced brackets in the alt, a shortcut reference) loses its "!" and is
// left a link, which needs a click. Removing one can close up a new one
// (![![x](y)](z), !![), so each pass repeats until none is left.
func dropImages(s string) string {
	var out, prose strings.Builder
	flush := func() {
		p := prose.String()
		for markdownImage.MatchString(p) {
			p = markdownImage.ReplaceAllString(p, "$1")
		}
		for strings.Contains(p, "![") {
			p = strings.ReplaceAll(p, "![", "[")
		}
		out.WriteString(p)
		prose.Reset()
	}
	fence := ""
	for _, line := range strings.SplitAfter(s, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case fence != "":
			out.WriteString(line)
			if closesFence(trimmed, fence) {
				fence = ""
			}
		case len(line)-len(trimmed) <= 3 && fenceOf(trimmed) != "":
			flush()
			fence = fenceOf(trimmed)
			out.WriteString(line)
		default:
			prose.WriteString(line)
		}
	}
	flush()
	return out.String()
}

// fenceOf is the run of three or more backticks or tildes a line opens a
// fenced code block with, or "".
func fenceOf(line string) string {
	for _, c := range []string{"`", "~"} {
		n := len(line) - len(strings.TrimLeft(line, c))
		if n >= 3 {
			return line[:n]
		}
	}
	return ""
}

// closesFence reports whether line closes the block fence opened: the same
// character, at least as many, nothing else.
func closesFence(line, fence string) bool {
	run := len(line) - len(strings.TrimLeft(line, fence[:1]))
	return run >= len(fence) && strings.TrimSpace(line[run:]) == ""
}

// dropAnswerImages strips the images from a scheduled run's answer and its
// reasoning, both rendered as markdown. Opening the thread would load them,
// so an image an injected page slipped in could carry what the run knows out
// in its URL, without a click.
func dropAnswerImages(out LoopResult) LoopResult {
	out.Content = dropImages(out.Content)
	out.ActivityTrace = dropReasoningImages(out.ActivityTrace)
	out.Blocks = slices.Clone(out.Blocks)
	for i := range out.Blocks {
		out.Blocks[i].Events = dropReasoningImages(out.Blocks[i].Events)
		if out.Blocks[i].Type == "text" {
			out.Blocks[i].Content = dropImages(out.Blocks[i].Content)
		}
	}
	return out
}

// dropReasoningImages is events with the images taken out of each
// reasoning text, on a copy: blocks and the trace may share them.
func dropReasoningImages(events []ActivityTraceEvent) []ActivityTraceEvent {
	events = slices.Clone(events)
	for i := range events {
		if events[i].Type == "reasoning" {
			events[i].Content = dropImages(events[i].Content)
		}
	}
	return events
}
