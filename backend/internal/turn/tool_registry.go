package turn

import (
	"context"
	"slices"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/sandbox"
)

// toolSpec is everything the engine knows about one tool. A built-in has a
// schema and a run; adding one is adding an entry here. An MCP tool's entry
// carries only the round policy: its server supplies the schema and runs it.
type toolSpec struct {
	name string
	// schema builds the definition offered to the model; nil for an MCP tool.
	schema func() llm.Tool
	// tool is schema's result, built once by registry and shared by every
	// turn: read-only, its Parameters map included.
	tool llm.Tool
	// offered reports whether the turn gets the tool; nil means always.
	offered func(e *Engine, thread chat.Thread, gate toolGate) bool
	// run executes a call and returns the model-facing output and the
	// artifacts it created; nil for an MCP tool.
	run toolRunFunc
	// capPerRound bounds how many calls of the tool one round runs; 0 means
	// maxToolCallsPerRound.
	capPerRound int
	// concurrent marks a stateless web read whose calls in a round may
	// overlap (see startToolRuns).
	concurrent bool
	// counts is the usage counter a successful MCP call bumps; nil counts
	// nothing. Built-ins count themselves.
	counts *usageCounter
	// userIntent marks a built-in that acts for the user beyond the turn (a
	// schedule saved, run or deleted): it refuses once outside content is in
	// the turn, which could be what asks for it (Run.readOutside).
	userIntent bool
	// sharesIntent marks a built-in whose output brings in no outside text,
	// so calling it first leaves a userIntent tool callable.
	sharesIntent bool
	// imageGenerator marks the image tool: an image turn forces it, a turn
	// makes one image, loom fills its loomArgs and locks the thread's model.
	imageGenerator bool
	// documentGenerator marks a document tool (docs/docgen.md). Their schemas
	// are the biggest chunk on offer, so the gate offers them only when the
	// turn's category or wording plausibly wants a downloadable file
	// (toolGate.docgenEnabled), and only where files can be stored. Its
	// argument can be a whole document, so it is long-running: the LLM
	// client widens its budgets while it is on offer (llm.Tool.LongRunning).
	// A scheduled run never gets one.
	documentGenerator bool
	// scheduledLastStep marks the one built-in a scheduled run also gets, as
	// its last step: it can read in full what the run's tools returned, so
	// once it did (its run sets Run.lastStepRan) the run calls no other tool
	// (Run.afterLastStep).
	scheduledLastStep bool

	// What follows is particular to an MCP tool loom knows by name (the web
	// tools, web_tools.go); nil or zero leaves the generic behaviour.

	// relabel cites a result in the tool's own way; it gets the result's
	// structuredContent.sources and decides (fetch uses them when sent, the
	// browser keeps its navigate/snapshot pairing either way). nil: the
	// structured sources, if any.
	relabel func(arguments map[string]any, output string, sources []mcp.Source, reg *webSourceRegistry) string
	// fallback retries a failed call another way; false keeps the failure.
	fallback func(ctx context.Context, t *Run, arguments map[string]any, round int, reg *webSourceRegistry) (string, bool)
	// scheduledClamp lowers the sizes a scheduled run's call asks for.
	scheduledClamp func(args map[string]any)
	// scheduledLimited counts the call against a scheduled run's paid-search
	// limit (scheduledSearchCalls).
	scheduledLimited bool
	// observe describes a finished call for a scheduled run's report.
	observe func(arguments map[string]any, sources []mcp.Source) ToolObservation
}

// usageCounter is one per-user usage counter: its label in the usage log and
// the store method that bumps it.
type usageCounter struct {
	label string
	inc   func(u UsageStore, ctx context.Context, userID string) error
}

type toolRunFunc func(ctx context.Context, t *Run, call llm.ToolCall) (string, []artifact.Response)

// coreTools are offered first, in this order.
var coreTools = []toolSpec{
	{
		// The cross-thread summarizer reads the other threads in the same
		// project, so only a project thread sees it.
		name:    ProjectThreadsToolName,
		schema:  projectThreadsTool,
		offered: func(_ *Engine, thread chat.Thread, _ toolGate) bool { return thread.ProjectID != nil },
		run: func(ctx context.Context, t *Run, _ llm.ToolCall) (string, []artifact.Response) {
			return t.e.projectThreadsDigest(ctx, t.user.ID, t.thread, t.zone()), nil
		},
	},
	// Cross-thread memory: conversation_search finds anything across the
	// user's whole history and read_thread loads one matched thread in full.
	// Their value is whole-history reach, so every thread gets them.
	{
		name:   conversationSearchToolName,
		schema: conversationSearchTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return t.e.conversationSearchDigest(ctx, t.user.ID, t.thread, args, t.zone())
		}),
	},
	{
		name:   readThreadToolName,
		schema: readThreadTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			threadID, _ := args["thread_id"].(string)
			return t.e.readThreadDigest(ctx, t.user.ID, threadID, t.zone())
		}),
	},
	// The directive tools manage the user's standing instructions. They are
	// whole-account, so every thread gets them.
	{
		name:   addUserDirectiveToolName,
		schema: addUserDirectiveTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.addUserDirectiveDigest(ctx, t.user.ID, args))
		}),
	},
	{
		name:   removeUserDirectiveToolName,
		schema: removeUserDirectiveTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.removeUserDirectiveDigest(ctx, t.user.ID, args))
		}),
	},
	{
		name:   replaceUserDirectiveToolName,
		schema: replaceUserDirectiveTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.replaceUserDirectiveDigest(ctx, t.user.ID, args))
		}),
	},
	// The fact tools change the user's memory on an explicit request. They are
	// whole-account, so every thread gets them.
	{
		name:   addUserFactToolName,
		schema: addUserFactTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.addUserFactDigest(ctx, t.user.ID, t.thread.ID, args))
		}),
	},
	{
		name:   updateUserFactToolName,
		schema: updateUserFactTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.updateUserFactDigest(ctx, t.user.ID, t.thread.ID, args))
		}),
	},
	{
		name:   forgetUserFactToolName,
		schema: forgetUserFactTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.forgetUserFactDigest(ctx, t.user.ID, args))
		}),
	},
}

// sandboxToolSpec is run_python: one small schema with its own guidance
// block, offered in every category since exact math or counting comes up
// anywhere. The gate leaves it out while the sidecar is unconfigured or
// unhealthy.
var sandboxToolSpec = toolSpec{
	name:    sandboxToolName,
	schema:  sandboxTool,
	offered: func(e *Engine, _ chat.Thread, gate toolGate) bool { return e.canStoreArtifacts() && gate.sandbox },
	run: func(ctx context.Context, t *Run, call llm.ToolCall) (string, []artifact.Response) {
		if t.scheduled == nil {
			return t.e.runSandboxTool(ctx, t.stream, t.user, t.thread, call, t.toolFiles, sandboxScope{documents: true})
		}
		// A scheduled run hands it tool files only, never a document: the
		// run reads untrusted pages, and a printed upload could leave
		// through a fetch. A job that got tool files is the run's last
		// step. Its jobs take turns on one sidecar slot, so runs that fire
		// together leave the other slots to chat; the soft deadline bounds
		// the wait and the job.
		ctx, cancel := t.roundContext(ctx)
		defer cancel()
		release, ok := t.e.scheduledSandboxSlot(ctx)
		if !ok {
			return "tool failed: cancelled", nil
		}
		defer release()
		return t.e.runSandboxTool(ctx, t.stream, t.user, t.thread, call, t.toolFiles, sandboxScope{
			beforeRun: func(files []sandbox.File) { t.lastStepRan = t.lastStepRan || len(files) > 0 },
		})
	},
	// Each job can hold a sandbox slot for up to a minute; a round that
	// wants more is better split across rounds.
	capPerRound:       sandboxToolCallsPerRound,
	scheduledLastStep: true,
}

// mcpPolicies are the policies of the MCP tools loom knows by name: the web
// tools (web_tools.go) and the image tool. Any other MCP tool gets the
// defaults.
var mcpPolicies = append(slices.Clone(webToolPolicies),
	// One image per turn; its image_gen count comes with the artifact.
	toolSpec{name: imageToolName, capPerRound: 1, imageGenerator: true},
)

// toolPolicy returns the spec that holds name's round policy: this engine's
// built-in of that name, else the MCP policy. A tool loom does not know by
// name gets the zero spec, i.e. the defaults.
func (s *Engine) toolPolicy(name string) *toolSpec {
	_, byName := s.registry()
	if spec := byName[name]; spec != nil {
		return spec
	}
	// A server's policy covers every tool it serves that has none of its own
	// (an mcp.json replacement may add tools the built-in never had).
	if server, _, ok := mcp.SplitExposedToolName(name); ok {
		if spec := serverPolicies[server]; spec != nil {
			return spec
		}
	}
	return &defaultPolicy
}

// defaultPolicy is the policy of a tool loom does not know: the defaults.
// Shared and read-only, like every policy toolPolicy returns.
var defaultPolicy toolSpec

// indexTools maps each spec by name; on a duplicate the first wins.
func indexTools(tools []toolSpec) map[string]*toolSpec {
	byName := make(map[string]*toolSpec, len(tools))
	for i := range tools {
		if _, exists := byName[tools[i].name]; !exists {
			byName[tools[i].name] = &tools[i]
		}
	}
	return byName
}

// registry returns this engine's built-ins in offer order, and by name every
// spec loom knows: those built-ins, then the MCP policies. It is built once,
// on first use, so an Engine literal works like one from New; so is each
// built-in's definition. On a duplicate name the first spec wins.
func (s *Engine) registry() ([]toolSpec, map[string]*toolSpec) {
	s.toolsOnce.Do(func() {
		tools := slices.Concat(coreTools, scheduleToolSpecs, []toolSpec{sandboxToolSpec})
		for i := range tools {
			tools[i].tool = tools[i].schema()
		}
		s.tools, s.toolsByName = tools, indexTools(slices.Concat(tools, mcpPolicies))
	})
	return s.tools, s.toolsByName
}

// canStoreArtifacts reports whether generated files have somewhere to go;
// the document, image and sandbox tools are offered only then.
func (s *Engine) canStoreArtifacts() bool {
	return s.artifacts != nil && strings.TrimSpace(s.usersDir) != ""
}

// withArgs adapts a built-in that takes JSON arguments and answers with a
// text digest: arguments that do not parse fail the call before it runs.
func withArgs(run func(ctx context.Context, t *Run, args map[string]any) string) toolRunFunc {
	return func(ctx context.Context, t *Run, call llm.ToolCall) (string, []artifact.Response) {
		args, err := parseToolArguments(call.Function.Arguments)
		if err != nil {
			return capToolOutput("tool failed: invalid arguments: " + err.Error()), nil
		}
		return run(ctx, t, args), nil
	}
}
