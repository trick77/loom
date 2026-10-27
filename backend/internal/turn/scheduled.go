package turn

import (
	"context"
	"encoding/json"
	"fmt"
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
	// SoftDeadline is when the loop stops offering tools and answers, cutting
	// a tool round still running then; zero means none.
	SoftDeadline time.Time
	// Memory is the schedule's research memory (sources, searches, last run)
	// for every call's system prompt.
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

// Bounds on a scheduled run's tools. Every tool result is re-read by each
// later round (a run once re-sent 131k of its 135k tokens), so a run keeps
// them small. Tavily is the one tool with a cost per call.
const (
	scheduledFetchMaxLength   = 8000
	scheduledSearchMaxResults = 5
	scheduledTavilyCalls      = 10
	scheduledToolResultBytes  = 12 << 10
)

// tavilyLimitReached is the tool result of a Tavily call past the run's limit.
var tavilyLimitReached = fmt.Sprintf("The search limit of this run (%d) is reached; answer from what you have.", scheduledTavilyCalls)

// toolRoundInference is a tool round's metadata. A scheduled run's rounds
// only pick tools, so they ask for the least reasoning (a run once spent 48s
// thinking about its next search); its answer always comes from the forced
// final, which keeps the turn's level.
func (t *Run) toolRoundInference(round int) llm.InferenceMetadata {
	meta := inferenceWithPurpose(t.inference, "chat_tool_round", round)
	meta.LeastReasoning = t.scheduled != nil
	return meta
}

// roundContext bounds a scheduled run's tool round by its soft deadline, so a
// model call still running at the deadline is cut and the run answers from
// what it has instead of reaching the hard limit.
func (t *Run) roundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	sr := t.scheduled
	if sr == nil || sr.SoftDeadline.IsZero() {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, sr.SoftDeadline.Sub(t.scheduledNow()))
}

func (t *Run) scheduledNow() time.Time {
	if t.scheduled != nil && t.scheduled.Now != nil {
		return t.scheduled.Now()
	}
	return time.Now()
}

// clampScheduledCall shrinks what a scheduled run's web call returns: a
// shorter fetch, fewer and shallower search results. The model picks these
// sizes itself and picks large ones.
func (t *Run) clampScheduledCall(call *llm.ToolCall) {
	if t.scheduled == nil {
		return
	}
	name := call.Function.Name
	if name != fetchToolName && name != tavilySearchExposedName {
		return
	}
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return
	}
	// Only a size the model set is lowered: the tools' own defaults are
	// smaller than the caps.
	switch name {
	case fetchToolName:
		if n, ok := args["max_length"].(float64); ok && n > scheduledFetchMaxLength {
			args["max_length"] = scheduledFetchMaxLength
		}
	case tavilySearchExposedName:
		if n, ok := args["max_results"].(float64); ok && n > scheduledSearchMaxResults {
			args["max_results"] = scheduledSearchMaxResults
		}
		args["search_depth"] = "basic"
		delete(args, "include_raw_content")
	}
	if raw, err := json.Marshal(args); err == nil {
		call.Function.Arguments = string(raw)
	}
}

// tavilyCapped counts a scheduled run's Tavily call and reports whether it is
// past the run's limit; such a call never starts.
func (t *Run) tavilyCapped(name string) bool {
	if t.scheduled == nil {
		return false
	}
	if server, _, ok := mcp.SplitExposedToolName(name); !ok || server != tavilyServerName {
		return false
	}
	if t.tavilyCalls >= scheduledTavilyCalls {
		return true
	}
	t.tavilyCalls++
	return false
}

// capScheduledOutput bounds a scheduled run's raw tool output before its
// sources are numbered, so no [n] points into a cut-off tail.
func (t *Run) capScheduledOutput(output string) string {
	if t.scheduled == nil || len(output) <= scheduledToolResultBytes {
		return output
	}
	return truncateBytesOnRuneBoundary(output, scheduledToolResultBytes-len(truncationEllipsis)) + truncationEllipsis
}

// scheduledRunPrompt tells the model it runs unattended.
// The run sees every server's guidance and, when it writes the answer, the
// user's standing instructions, both written for a live chat, so it says
// which parts still apply.
const scheduledRunPrompt = "You are running a scheduled task on your own: nobody is waiting in the chat and nobody can answer a question before the next run. Research with the tools this task needs, then answer with what is new in the time span the task names, or since the last run when it names none, naming each item with its source. When nothing is new, say so in one sentence. Never ask the user anything. These rules of the task come before any standing instruction or tool guidance written for a live chat; follow those only where they fit an unattended run, such as the answer's language or tone."

// scheduledResearchNote tells the rounds that offer tools not to write the
// answer: a scheduled run's answer always comes from the tool-free final
// call, the only one that sees the user memory, so a round's own answer is
// dropped unread. withFinalContext takes it out again for that call.
const scheduledResearchNote = "While tools are offered, only research: once you have what the task needs, reply with the single word DONE instead of an answer. A separate step writes the answer from your research."

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
	if !sr.SoftDeadline.IsZero() && !t.scheduledNow().Before(sr.SoftDeadline) {
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

// withFinalContext is history for a scheduled run's call that offers no
// tools and writes the answer: the research-only note taken out of the
// system prompt, and the user memory and directives added.
// Any other turn's history comes back as it is.
func (t *Run) withFinalContext(history []llm.Message) []llm.Message {
	if t.scheduled == nil {
		return history
	}
	out := slices.Clone(history)
	if len(out) > 0 && out[0].Role == "system" {
		out[0].Content = joinNonEmptyBlocks(strings.Replace(out[0].Content, scheduledResearchNote, "", 1), t.plan.finalContext)
		return out
	}
	if t.plan.finalContext == "" {
		return out
	}
	return append([]llm.Message{{Role: "system", Content: t.plan.finalContext}}, out...)
}

// notesBudgetTokens caps the research notes of the turn's final call.
func (t *Run) notesBudgetTokens() int {
	if t.scheduled != nil {
		return scheduledNotesBudgetTokens
	}
	return finalSynthesisNotesBudgetTokens
}

// markdownImage is an inline or reference markdown image: ![alt](src) or
// ![alt][ref].
var markdownImage = regexp.MustCompile(`!\[([^\]]*)\](\([^)]*\)|\[[^\]]*\])`)

// dropImages leaves no markdown image: a plain one keeps its alt text, and
// any other image marker (balanced brackets in the alt, a shortcut
// reference) loses its "!" and is left a link, which needs a click. Every
// image starts with "![", so none is left whatever the renderer makes of the
// rest; code blocks are not spared, as a guess at where one ends can be
// wrong. Removing one can close up a new one (![![x](y)](z), !![), so each
// pass repeats until none is left.
func dropImages(s string) string {
	for markdownImage.MatchString(s) {
		s = markdownImage.ReplaceAllString(s, "$1")
	}
	for strings.Contains(s, "![") {
		s = strings.ReplaceAll(s, "![", "[")
	}
	return s
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
