package turn

import (
	"context"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// Run is one persisted chat turn's fixed state: who asks, in which thread,
// where its events go, and the routing Prepare decided. Prepare is the only way
// to get one, so the assistant loop and persistence never run on a turn whose
// prompt was not assembled. What varies per call (ctx, a round's history, a
// tool call) stays a parameter. The incognito turn has no Run; see
// RunIncognitoTurn.
type Run struct {
	e      *Engine
	stream Emitter
	// toolTitles is the server-given title each tool call was streamed with,
	// by call id: the persisted trace stores exactly what the user saw live.
	toolTitles map[string]string
	titles     *ReasoningTitleTracker
	inference  llm.InferenceMetadata
	user       auth.User
	thread     chat.Thread
	// userMessage is the persisted user message this turn answers.
	userMessage chat.Message
	// plan is what Prepare assembled for the assistant loop.
	plan turnPlan
	// offered is the built-ins the turn offered, by name; dispatch runs a
	// built-in only through it (see offer).
	offered map[string]*toolSpec
	// offeredDocTools are the document tools the turn offered, by name: a
	// call to one the gate left out never runs (documentGenerator).
	offeredDocTools map[string]bool
	// acc sums every model call of the turn; start times its wall clock.
	acc   *llm.UsageAccumulator
	start time.Time
	// inbox is where the turn picks up queued messages; nil when nothing can
	// queue.
	inbox Inbox
	// timezone is the user's IANA zone (RunConfig.Timezone); "" when unknown.
	// loc is it resolved (zone).
	timezone string
	loc      *time.Location
	// scheduled is set when the turn is a schedule's headless run.
	scheduled *ScheduledRun
	// scheduledTools are the tools a scheduled run was offered, by name; it
	// runs no other.
	scheduledTools map[string]bool
	// searchCalls counts the paid search calls a scheduled run started.
	searchCalls int
	// lastStepRan is set once a scheduled run's scheduledLastStep tool read
	// the run's tool files; it calls no other tool after that (afterLastStep).
	// A job on the model's own data, or one refused before it started, sets
	// nothing: it prints nothing the run's tools did not already show.
	lastStepRan bool
	// readOutside is set once any tool but an acting built-in (schedules,
	// memory) returned content into the turn, or from the start when the
	// turn answers a message carrying a quoted passage (answersOutside): from
	// then on its text may carry instructions the user never gave, and no
	// acting call runs (Engine.acts).
	readOutside bool
	// toolFiles are the files MCP tools handed to run_python this turn, by
	// name (keepToolFiles); dropped with the turn. A turn is one user's, so
	// no other user's run reaches them.
	toolFiles     map[string]mcp.File
	toolFileBytes int
	// outdoorCharted is set once an outdoor_windows result asked for its
	// chart: one chart a turn, not one per call (today, tomorrow, week).
	outdoorCharted bool
	// ideaCards are the cards the running idea_propose call proposed; the
	// loop moves them into the turn's blocks and the stream.
	ideaCards []IdeaCard
	// ideasRead is set once ideas_lookup ran: an idea's text may be a page's
	// words saved with one click, so it counts as outside content for every
	// acting tool but a further lookup (refusesAction) and for the answer's
	// read_outside flag.
	ideasRead bool
}

// RunConfig is a persisted turn's fixed state as the stream handler knows it.
// The turn's model calls are attributed to User and Thread.
type RunConfig struct {
	Stream Emitter
	Titles *ReasoningTitleTracker
	User   auth.User
	Thread chat.Thread
	// UserMessage is the persisted user message this turn answers.
	UserMessage chat.Message
	// Usage sums every model call of the turn; Start times its wall clock.
	Usage *llm.UsageAccumulator
	Start time.Time
	// Inbox, when set, lets the turn pick up the messages the user queues
	// while it runs (see Inbox).
	Inbox Inbox
	// Timezone is the user's IANA zone: the asking browser's for a chat turn,
	// the schedule's for a scheduled run. The prompt's date reads in it and the
	// schedule tools draw slots in it; "" reads UTC.
	Timezone string
	// Scheduled makes the turn a schedule's headless run; nil for a chat turn.
	Scheduled *ScheduledRun
}

// PrepareInput is what the prompt-assembly phase of a persisted turn works from.
type PrepareInput struct {
	// StreamCtx is the turn's cancellable context (usage accumulator attached);
	// TurnCtx carries the same attribution on the request context for the
	// helpers that must not be cancelled by a stop; ReqCtx is the bare request
	// context.
	StreamCtx context.Context
	TurnCtx   context.Context
	ReqCtx    context.Context
	// The attachment ids are the user's send as requested; its text is
	// RunConfig.UserMessage.
	ImageAttachmentIDs    []string
	DocumentAttachmentIDs []string
	PriorMessages         []chat.Message
	// Picked are the queued messages this turn answers along with its user
	// message (taken from the queue together; also in PriorMessages). A
	// quoted passage among them locks the turn and stays in its history; an
	// earlier one, answered or not, never does. Nil for a plain send.
	Picked []chat.Message
	// ImageParts are the vision parts for the images the user attached this
	// turn, resolved (and validated) before anything was persisted.
	ImageParts []llm.MessageContentPart
}

// turnPlan is the assembled prompt and the routing decisions the assistant
// loop runs with.
type turnPlan struct {
	history    []llm.Message
	imageRoute imageRouting
	gate       toolGate
	// tools is the turn's tool set (offer): the tool guidance and every
	// round read this one set.
	tools            []llm.Tool
	editSource       *editImageSource
	knowledgeSources []Citation
	// sourceCount is how many [n] markers the attached and knowledge documents
	// took, so web sources continue the numbering.
	sourceCount int
	// finalContext is a scheduled run's user memory and directives, for the
	// tool-free call that writes its answer only; "" otherwise.
	finalContext string
	// researchBlock is what a scheduled run's tool rounds read and its
	// answer step must not: the research note and the run_python guidance,
	// as put into the system prompt (withFinalContext strips exactly this).
	researchBlock string
}

// Prepare starts a persisted turn on the engine: it classifies the turn, gates
// the tools, gathers every context block (user, project, attached documents,
// project knowledge, RAG) and builds the model history, and returns the Run
// that answers and persists it. It emits the knowledge_sources event as a side
// effect, since the sources are known here and the client wants them before
// the first token.
func (s *Engine) Prepare(c RunConfig, in PrepareInput) *Run {
	t := &Run{
		e:      s,
		stream: c.Stream,
		titles: c.Titles,
		inference: llm.InferenceMetadata{
			UserID:   c.User.ID,
			Username: c.User.Username,
			ThreadID: c.Thread.ID,
			// The thread's slot, picked in the composer. A deployment without
			// a deep model routes it to the chat model (llm.Client).
			Deep: c.Thread.ChatSlot == llm.SlotDeep,
		},
		user:        c.User,
		thread:      c.Thread,
		userMessage: c.UserMessage,
		acc:         c.Usage,
		start:       c.Start,
		inbox:       c.Inbox,
		timezone:    c.Timezone,
		loc:         zoneLocation(c.Timezone),
		scheduled:   c.Scheduled,
		// A question that carries outside text along (an idea's passage,
		// flagged on insert) is answered like a turn that read a page: no
		// action, and the answer is kept out of fact extraction.
		readOutside: answersOutside(c.UserMessage, in.Picked),
	}
	t.plan = t.prepare(in)
	return t
}

// prepare assembles the turn's plan; see Prepare.
func (t *Run) prepare(in PrepareInput) turnPlan {
	imageParts := in.ImageParts
	// own is the message as the routing calls read it: a quoted passage
	// (outside text) left out, so it cannot pick the category, the tools or
	// a forced image generation.
	own := chat.WithoutOutsideText(t.userMessage)
	// category drives the prompt-classifier block injected below. On the first
	// message we classify now (before the answer history is built) and use the
	// fresh result; on later turns we reuse the stored category.
	//
	// The condition is this being the thread's first turn AND its category never
	// having been set. It used to be ShouldGenerateThreadTitle, a proxy for "first
	// turn" that held only because the UI creates threads titled with the raw
	// first message — and that leaked: a later turn whose text matched the stored
	// title re-ran the classifier and overwrote the label. CreateThread never sets
	// category, so an empty one is the honest "never classified" signal.
	//
	// Both halves are needed. Without the message check, every pre-existing thread
	// with an unset category (there is no backfill migration) would classify on
	// its next turn and stamp that turn's text as the thread's sticky identity —
	// on a long thread that label is likely wrong, and freshlyClassified would
	// suppress the per-turn drift re-classification below on the same turn. A
	// thread's category describes what it opened with, so it is set on turn one or
	// not at all; later drift is handled per-turn just below. Titling has moved
	// after the answer and no longer shares this gate.
	category := t.thread.Category
	// A scheduled run is research with the user's MCP tools, plus run_python
	// on the files those tools return (never the user's uploads): no image
	// route and no drift check. It classifies its prompt on every run, so
	// category-tagged servers are offered as in chat, and keeps the label to
	// itself: an edited prompt must never inherit a stale one.
	scheduled := t.scheduled != nil
	freshlyClassified := !scheduled && len(in.PriorMessages) == 0 && strings.TrimSpace(category) == ""

	// Every pre-answer load below is independent of the others, and each may be
	// a model or database round trip, so they run concurrently: the answer
	// waits for the slowest, not their sum. The tool gate and the history need
	// all of them and are built after the join. The document chain stays
	// sequential inside its goroutine: attached documents, then project
	// knowledge, then RAG share the [n] numbering in docIdx.
	//
	// The image-intent gate (language-agnostic; see classifyImageTurn) decides
	// whether this turn routes to image generation/editing. An image turn's
	// category is stamped deterministically as image_generation and its tools
	// are forced, so the two classifiers are wasted on it — but image turns are
	// rare, and running the classifiers alongside the gate instead of after it
	// takes a whole gate round trip off every other first turn. Their results
	// are simply dropped on an image turn below.
	var (
		imageRoute                        imageRouting
		classified                        string
		turnCategory                      string
		userContext                       string
		projectContext                    string
		sandboxGuidance                   string
		docIdx                            = newDocIndexer()
		documentContext, knowledgeContext string
		knowledgeSources                  []Citation
		sandboxOn                         = t.e.sandboxOffered()
	)
	parallel(
		func() {
			if !scheduled {
				imageRoute = t.e.classifyImageTurn(in.StreamCtx, t.inference, own, len(in.ImageAttachmentIDs) > 0, in.PriorMessages)
			}
		},
		func() {
			switch {
			case scheduled:
				classified = t.e.classifyScheduledRun(in.StreamCtx, t.inference, own)
			case freshlyClassified:
				classified = t.e.classifyFirstTurn(in.StreamCtx, t.inference, own)
			}
		},
		// Semantic drift detection: on a continued turn whose sticky category does
		// not already grant every category-gated tool, re-classify THIS message
		// so a thread that drifted (into coding/how-to, into music, in any
		// language) still gets context7, the music tools et al. This reuses the
		// same model classifier as the first message — no hand-maintained
		// keyword lexicon. Skipped when the turn is being classified fresh, or
		// when the sticky category already grants the coding-doc tools and every
		// other gated server this user may use. Fail-safe: ClassifyThread returns
		// General on failure, so a failed classification simply adds nothing.
		func() {
			if scheduled || freshlyClassified ||
				(categoryGrantsCodingDocs(category) && (t.e.mcp == nil || t.e.mcp.CoversGated(MCPPrincipal(t.user), category))) {
				return
			}
			driftInference := gateInference(t.inference, "classify_drift")
			driftCtx, cancelDrift := context.WithTimeout(in.StreamCtx, t.e.gateTimeout())
			defer cancelDrift()
			turnCategory, _ = t.e.llm.ClassifyThread(llm.WithInferenceMetadata(driftCtx, driftInference), own)
		},
		func() { userContext = t.e.memory.UserContext(in.ReqCtx, t.user.ID) },
		// A scheduled run brings no project memory, knowledge or documents
		// into rounds that read untrusted pages.
		func() {
			if !scheduled {
				projectContext = t.e.memory.ProjectContext(in.ReqCtx, t.user.ID, t.thread)
			}
		},
		// run_python's guidance travels with the tool: when the sidecar is
		// off, the prompt never mentions it. A scheduled run lists no
		// documents: run_python reads only its tool files there.
		func() {
			if sandboxOn && scheduled {
				sandboxGuidance = sandboxGuidancePrompt
			} else if sandboxOn {
				sandboxGuidance = t.e.sandboxGuidance(in.ReqCtx, t.user.ID, t.thread, in.DocumentAttachmentIDs)
			}
		},
		func() {
			if scheduled {
				return
			}
			var inlinedDocIDs, knowledgeInlinedIDs map[string]bool
			var attachmentSources []Citation
			var inlinedAll bool
			documentContext, inlinedDocIDs, attachmentSources = t.e.documentInlineContext(in.TurnCtx, t.user.ID, t.thread, in.DocumentAttachmentIDs, docIdx)
			knowledgeContext, knowledgeInlinedIDs, knowledgeSources, inlinedAll = t.e.knowledgeInlineContext(in.TurnCtx, t.user.ID, t.thread, inlinedDocIDs, docIdx)
			if !inlinedAll {
				ragExclude := mergeDocIDSets(inlinedDocIDs, knowledgeInlinedIDs)
				ragContext, ragSources := t.e.knowledgeContextForThread(in.TurnCtx, t.user.ID, t.thread, t.userMessage.Content, ragExclude, docIdx)
				knowledgeContext = joinNonEmptyBlocks(knowledgeContext, ragContext)
				knowledgeSources = append(knowledgeSources, ragSources...)
			}
			knowledgeSources = append(append([]Citation(nil), attachmentSources...), knowledgeSources...)
		},
	)
	// Attached and knowledge documents are not outside content: only the
	// user uploads them (documents.Upload), and counting them locked every
	// action in a thread or project with documents for good, since every
	// turn there retrieves excerpts. Only tool results set readOutside.
	if t.readOutside {
		// The forced image route runs the generator without the action
		// check: a turn that answers outside text gets no forced action.
		imageRoute = imageRouting{}
	}
	imageArtifactRequired := imageRoute.generate
	if imageArtifactRequired {
		// The image path forces its own tools; the drift guess adds nothing.
		turnCategory = ""
	}
	if freshlyClassified {
		category = classified
		if imageArtifactRequired {
			category = string(classifier.ImageGeneration)
		}
		t.e.persistThreadCategory(context.WithoutCancel(in.ReqCtx), t.user, t.thread.ID, category)
	}
	if scheduled {
		category = classified
	}

	gate := newToolGate(category, turnCategory, own)
	gate.sandbox = sandboxOn
	tools := t.offer(gate)
	fileToolGuidance := ""
	// A scheduled run has no file tools to guide, nor has a turn the gate or
	// a sidecar that is down leaves without them.
	if len(t.offeredDocTools) > 0 {
		fileToolGuidance = fileToolGuardrailPrompt
	}
	if len(knowledgeSources) > 0 {
		_ = t.stream.SendJSON("knowledge_sources", map[string]any{"sources": knowledgeSources})
	}
	scheduleBlock := ""
	// A scheduled run reads untrusted pages with tools nobody watches, so the
	// user memory and directives stay out of every call that offers tools:
	// only the tool-free call that writes the answer gets them (finalContext).
	finalContext, researchBlock := "", ""
	if scheduled {
		// The run_python guidance goes with the research note: the answer
		// step offers no tools, and a prompt still saying "You have
		// run_python" there gets a chart promised instead of drawn.
		researchBlock = joinNonEmptyBlocks(researchNote(sandboxOn), sandboxGuidance)
		sandboxGuidance = ""
		scheduleBlock = joinNonEmptyBlocks(scheduledRunPrompt, t.startedNote(), t.sinceNote(), researchBlock, t.scheduled.Memory)
		finalContext, userContext = userContext, ""
		if sandboxOn {
			finalContext = joinNonEmptyBlocks(finalContext, scheduledChartAnswerNote)
		}
	}
	history := buildLLMHistory(t.user, t.now(), promptBlocks{
		toolGuidance: joinNonEmptyBlocks(fileToolGuidance, sandboxGuidance, t.viewImageGuidance(), t.e.mcpServerGuidance(tools, t.offered)),
		schedule:     scheduleBlock,
		classifier:   classifier.Block(category),
		user:         userContext,
		project:      projectContext,
		knowledge:    knowledgeContext,
		document:     documentContext,
	}, in.PriorMessages, t.userMessage, answering(in.Picked))
	// editSourceID is the image whose original pixels are forwarded to the image
	// model for direct editing (image-to-image). Defaults to the photo the user
	// attached this turn; the follow-up branch below sets it to a reused prior image.
	editSourceID := ""
	if len(in.ImageAttachmentIDs) > 0 {
		editSourceID = in.ImageAttachmentIDs[0]
	}
	// Silently reuse the conversation's most recent image as the model's vision
	// input when this turn is a follow-up edit/restyle ("make it cyberpunk",
	// "create a variation") and the user attached nothing explicitly — so editing
	// the just-generated image needs no manual re-attach step. Fresh creation
	// requests never pull in a prior image. This is best-effort: if the source
	// can't be loaded the turn proceeds text-only rather than failing, unlike an
	// explicit attachment (handled above) whose failure is surfaced to the user.
	if len(imageParts) == 0 && imageRoute.reuseSource {
		if sourceID := latestImageArtifactID(in.PriorMessages); sourceID != "" {
			if parts, partsErr := t.e.imageContentParts(in.ReqCtx, t.user.ID, t.userMessage.Content, []string{sourceID}); partsErr != nil {
				slog.Warn("auto-attach of prior image failed; continuing without source image",
					"thread_id", t.thread.ID, "artifact_id", sourceID, "err", partsErr)
			} else {
				imageParts = parts
				editSourceID = sourceID
			}
		}
	}
	if len(imageParts) > 0 {
		history[len(history)-1].Content = ""
		// The parts carry the bare user text: the earlier links go along.
		if note := earlierSourcesNote(in.PriorMessages); note != "" {
			imageParts = append(slices.Clip(imageParts), llm.MessageContentPart{Type: "text", Text: strings.TrimSpace(note)})
		}
		history[len(history)-1].ContentParts = imageParts
	}
	// When the image path will run, forward the source image's original full-res
	// bytes so the model edits the actual pixels instead of a lossy text
	// re-description. Best-effort: on failure the turn still generates, just without
	// the source image.
	var editSource *editImageSource
	if imageArtifactRequired && editSourceID != "" {
		if src, ok, srcErr := t.e.loadEditSourceImage(in.ReqCtx, t.user.ID, editSourceID); srcErr != nil {
			slog.Warn("load edit source image failed; generating without source image",
				"thread_id", t.thread.ID, "artifact_id", editSourceID, "err", srcErr)
		} else if ok {
			editSource = &src
		}
	}
	return turnPlan{
		history:          history,
		imageRoute:       imageRoute,
		gate:             gate,
		tools:            tools,
		editSource:       editSource,
		knowledgeSources: knowledgeSources,
		sourceCount:      docIdx.count(),
		finalContext:     finalContext,
		researchBlock:    researchBlock,
	}
}

// ImageRequired reports whether Prepare routed the turn to image generation,
// so a turn that ends without an image artifact has failed.
func (t *Run) ImageRequired() bool {
	return t.plan.imageRoute.generate
}

// parallel runs fns concurrently and returns once all have finished. A panic
// in any of them is re-raised on the caller's goroutine, where the stream
// handler's recover turns it into an error event; left on its own goroutine
// it would take the process down.
func parallel(fns ...func()) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var panicked any
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					// The re-panic below happens on the caller's goroutine, so
					// whoever recovers it sees that stack; log this one, which
					// names the code that actually failed.
					slog.Error("panic in a pre-answer load", "panic", r, "stack", string(debug.Stack()))
					mu.Lock()
					if panicked == nil {
						panicked = r
					}
					mu.Unlock()
				}
			}()
			fn()
		}()
	}
	wg.Wait()
	if panicked != nil {
		panic(panicked)
	}
}
