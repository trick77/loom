package turn

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
)

// Run is one persisted chat turn's fixed state: who asks, in which thread,
// where its events go, and the routing Prepare decided. Prepare is the only way
// to get one, so the assistant loop and persistence never run on a turn whose
// prompt was not assembled. What varies per call (ctx, a round's history, a
// tool call) stays a parameter. The incognito turn has no Run; see
// RunIncognitoTurn.
type Run struct {
	e         *Engine
	stream    Emitter
	titles    *ReasoningTitleTracker
	inference llm.InferenceMetadata
	user      auth.User
	thread    chat.Thread
	// userMessage is the persisted user message this turn answers.
	userMessage chat.Message
	// plan is what Prepare assembled for the assistant loop.
	plan turnPlan
	// acc sums every model call of the turn; start times its wall clock.
	acc   *llm.UsageAccumulator
	start time.Time
}

// RunConfig is a persisted turn's fixed state as the stream handler knows it.
type RunConfig struct {
	Stream    Emitter
	Titles    *ReasoningTitleTracker
	Inference llm.InferenceMetadata
	User      auth.User
	Thread    chat.Thread
	// UserMessage is the persisted user message this turn answers.
	UserMessage chat.Message
	// Usage sums every model call of the turn; Start times its wall clock.
	Usage *llm.UsageAccumulator
	Start time.Time
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
	// Content and the attachment ids are the user's send as requested.
	Content               string
	ImageAttachmentIDs    []string
	DocumentAttachmentIDs []string
	PriorMessages         []chat.Message
	// ImageParts are the vision parts for the images the user attached this
	// turn, resolved (and validated) before anything was persisted.
	ImageParts []llm.MessageContentPart
}

// turnPlan is the assembled prompt and the routing decisions the assistant
// loop runs with.
type turnPlan struct {
	history          []llm.Message
	imageRoute       imageRouting
	gate             toolGate
	editSource       *editImageSource
	knowledgeSources []Citation
	// sourceCount is how many [n] markers the attached and knowledge documents
	// took, so web sources continue the numbering.
	sourceCount int
}

// Prepare starts a persisted turn on the engine: it classifies the turn, gates
// the tools, gathers every context block (user, project, attached documents,
// project knowledge, RAG) and builds the model history, and returns the Run
// that answers and persists it. It emits the knowledge_sources event as a side
// effect, since the sources are known here and the client wants them before
// the first token.
func (s *Engine) Prepare(c RunConfig, in PrepareInput) *Run {
	t := &Run{
		e:           s,
		stream:      c.Stream,
		titles:      c.Titles,
		inference:   c.Inference,
		user:        c.User,
		thread:      c.Thread,
		userMessage: c.UserMessage,
		acc:         c.Usage,
		start:       c.Start,
	}
	t.plan = t.prepare(in)
	return t
}

// prepare assembles the turn's plan; see Prepare.
func (t *Run) prepare(in PrepareInput) turnPlan {
	imageParts := in.ImageParts
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
	freshlyClassified := len(in.PriorMessages) == 0 && strings.TrimSpace(category) == ""

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
			imageRoute = t.e.classifyImageTurn(in.StreamCtx, t.inference, in.Content, len(in.ImageAttachmentIDs) > 0, in.PriorMessages)
		},
		func() {
			if freshlyClassified {
				classified = t.e.classifyFirstTurn(in.StreamCtx, t.inference, t.userMessage.Content)
			}
		},
		// Semantic drift detection: on a continued turn whose sticky category does
		// not already grant the coding-doc tools, re-classify THIS message so a
		// thread that drifted into coding/how-to (in any language) still gets
		// context7 et al. This reuses the same model classifier as the first
		// message — no hand-maintained keyword lexicon. Skipped when the turn is
		// being classified fresh or when the sticky category already grants those
		// tools. Fail-safe: ClassifyThread returns General on failure, so a failed
		// classification simply adds nothing.
		func() {
			if freshlyClassified || categoryGrantsCodingDocs(category) {
				return
			}
			driftInference := gateInference(t.inference, "classify_drift")
			driftCtx, cancelDrift := context.WithTimeout(in.StreamCtx, t.e.gateTimeout())
			defer cancelDrift()
			turnCategory, _ = t.e.llm.ClassifyThread(llm.WithInferenceMetadata(driftCtx, driftInference), t.userMessage.Content)
		},
		func() { userContext = t.e.memory.UserContext(in.ReqCtx, t.user.ID) },
		func() { projectContext = t.e.memory.ProjectContext(in.ReqCtx, t.user.ID, t.thread) },
		// run_python's guidance travels with the tool: when the sidecar is
		// off, the prompt never mentions it.
		func() {
			if sandboxOn {
				sandboxGuidance = t.e.sandboxGuidance(in.ReqCtx, t.user.ID, t.thread, in.DocumentAttachmentIDs)
			}
		},
		func() {
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

	gate := newToolGate(category, turnCategory, t.userMessage.Content)
	gate.sandbox = sandboxOn
	fileToolGuidance := ""
	if gate.docgenEnabled() {
		fileToolGuidance = fileToolGuardrailPrompt
	}
	if len(knowledgeSources) > 0 {
		_ = t.stream.Send("knowledge_sources", map[string]any{"sources": knowledgeSources})
	}
	history := buildLLMHistory(t.user, promptBlocks{
		toolGuidance: joinNonEmptyBlocks(fileToolGuidance, sandboxGuidance),
		classifier:   classifier.Block(category),
		user:         userContext,
		project:      projectContext,
		knowledge:    knowledgeContext,
		document:     documentContext,
	}, in.PriorMessages, t.userMessage)
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
		editSource:       editSource,
		knowledgeSources: knowledgeSources,
		sourceCount:      docIdx.count(),
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
