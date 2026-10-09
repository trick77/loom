package turn

import (
	"context"
	"slices"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
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
}

// usageCounter is one per-user usage counter: its label in the usage log and
// the store method that bumps it.
type usageCounter struct {
	label string
	inc   func(u UsageStore, ctx context.Context, userID string) error
}

type toolRunFunc func(ctx context.Context, t *Run, call llm.ToolCall) (string, []artifact.Response)

// coreTools are offered ahead of the generated-file tools, in this order.
var coreTools = []toolSpec{
	{
		// The cross-thread summarizer reads the other threads in the same
		// project, so only a project thread sees it.
		name:    ProjectThreadsToolName,
		schema:  projectThreadsTool,
		offered: func(_ *Engine, thread chat.Thread, _ toolGate) bool { return thread.ProjectID != nil },
		run: func(ctx context.Context, t *Run, _ llm.ToolCall) (string, []artifact.Response) {
			return t.e.projectThreadsDigest(ctx, t.user.ID, t.thread), nil
		},
	},
	// Cross-thread memory: conversation_search finds anything across the
	// user's whole history and read_thread loads one matched thread in full.
	// Their value is whole-history reach, so every thread gets them.
	{
		name:   conversationSearchToolName,
		schema: conversationSearchTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return t.e.conversationSearchDigest(ctx, t.user.ID, t.thread, args)
		}),
	},
	{
		name:   readThreadToolName,
		schema: readThreadTool,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			threadID, _ := args["thread_id"].(string)
			return t.e.readThreadDigest(ctx, t.user.ID, threadID)
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
		return t.e.runSandboxTool(ctx, t.stream, t.user, t.thread, call)
	},
	// Each job can hold a sandbox slot for up to a minute; a round that
	// wants more is better split across rounds.
	capPerRound: sandboxToolCallsPerRound,
}

// mcpPolicies are the policies of the MCP tools loom knows by name. fetch and
// obscura are very inexpensive (an HTTP read, a headless page load), so they
// get a higher cap than the default that guards pricier tools. An obscura page
// load counts per navigate; the fetch->obscura fallback navigates outside the
// tool loop and counts itself (see fetchObscuraFallback).
var mcpPolicies = []toolSpec{
	{
		name:        fetchToolName,
		capPerRound: cheapToolCallsPerRound,
		concurrent:  true,
		counts:      &usageCounter{"web_fetch", UsageStore.IncWebFetch},
	},
	{
		name:       tavilySearchExposedName,
		concurrent: true,
		counts:     &usageCounter{"web_search", UsageStore.IncWebSearch},
	},
	{
		name:        obscuraNavigateToolName,
		capPerRound: cheapToolCallsPerRound,
		counts:      &usageCounter{"obscura_fetch", UsageStore.IncObscuraFetch},
	},
	{name: obscuraSnapshotToolName, capPerRound: cheapToolCallsPerRound},
}

// toolPolicy returns the spec that holds name's round policy: this engine's
// built-in of that name, else the MCP policy. A tool loom does not know by
// name gets the zero spec, i.e. the defaults.
func (s *Engine) toolPolicy(name string) toolSpec {
	_, byName := s.registry()
	if spec := byName[name]; spec != nil {
		return *spec
	}
	return toolSpec{}
}

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

// docToolSpec offers a file generator. They are the biggest built-in schema
// chunk, so the gate offers them only when the turn's category or wording
// plausibly wants a downloadable file. Its argument can be a whole document, so
// it is long-running: the LLM client widens its budgets while it is on offer
// (see llm.Tool.LongRunning).
func docToolSpec(gen docgen.Generator) toolSpec {
	return toolSpec{
		name: gen.ToolName(),
		schema: func() llm.Tool {
			s := gen.Schema()
			tool := toolFromSchema(s.Name, s.Description, s.Parameters)
			tool.LongRunning = true
			return tool
		},
		offered: func(e *Engine, _ chat.Thread, gate toolGate) bool {
			return e.canStoreArtifacts() && gate.docgenEnabled()
		},
		run: func(ctx context.Context, t *Run, call llm.ToolCall) (string, []artifact.Response) {
			output, resp := t.runDocGenerator(ctx, call, gen)
			return output, oneArtifact(resp)
		},
	}
}

// imageToolSpec offers an image generator in every turn: one small schema,
// and the image path forces it separately when a turn requires an image.
func imageToolSpec(gen imagegen.Tool) toolSpec {
	return toolSpec{
		name: gen.ToolName(),
		schema: func() llm.Tool {
			s := gen.Schema()
			return toolFromSchema(s.Name, s.Description, s.Parameters)
		},
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return e.canStoreArtifacts() },
		run: func(ctx context.Context, t *Run, call llm.ToolCall) (string, []artifact.Response) {
			output, resp := t.executeImageTool(ctx, call, gen)
			return output, oneArtifact(resp)
		},
	}
}

// registry returns this engine's built-ins in offer order, and by name every
// spec loom knows: those built-ins, then the MCP policies. It is built once,
// on first use, so an Engine literal works like one from New; so is each
// built-in's definition. On a duplicate name the first spec wins.
func (s *Engine) registry() ([]toolSpec, map[string]*toolSpec) {
	s.toolsOnce.Do(func() {
		tools := slices.Clone(coreTools)
		for _, gen := range s.docTools {
			tools = append(tools, docToolSpec(gen))
		}
		tools = append(tools, sandboxToolSpec)
		for _, gen := range s.imageTools {
			tools = append(tools, imageToolSpec(gen))
		}
		for i := range tools {
			tools[i].tool = tools[i].schema()
		}
		s.tools, s.toolsByName = tools, indexTools(slices.Concat(tools, mcpPolicies))
	})
	return s.tools, s.toolsByName
}

// canStoreArtifacts reports whether generated files have somewhere to go;
// the file, image and sandbox tools are offered only then.
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

// toolFromSchema builds the function tool a leaf package's schema describes.
func toolFromSchema(name, description string, parameters map[string]any) llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}
