package httpapi

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

// turnRun is one chat turn's fixed state: who asks, in which thread, where its
// events go, and the routing prepareTurn decided. It is built once per turn;
// what varies per call (ctx, a round's history, a tool call) stays a parameter.
// The incognito turn uses a reduced form with no thread, message or plan.
type turnRun struct {
	s         *Engine
	stream    Emitter
	titles    *reasoningTitleTracker
	inference llm.InferenceMetadata
	user      auth.User
	thread    chat.Thread
	// userMessage is the persisted user message this turn answers.
	userMessage chat.Message
	// plan is set from prepareTurn's result before the assistant loop runs.
	plan turnPlan
	// usage sums every model call of the turn; start times its wall clock.
	usage *llm.UsageAccumulator
	start time.Time
}

// turnInput is what the prompt-assembly phase of a persisted turn works from.
type turnInput struct {
	// streamCtx is the turn's cancellable context (usage accumulator attached);
	// turnCtx carries the same attribution on the request context for the
	// helpers that must not be cancelled by a stop; reqCtx is the bare request
	// context.
	streamCtx context.Context
	turnCtx   context.Context
	reqCtx    context.Context
	// content and the attachment ids are the user's send as requested.
	content               string
	imageAttachmentIDs    []string
	documentAttachmentIDs []string
	priorMessages         []chat.Message
	// imageParts are the vision parts for the images the user attached this
	// turn, resolved (and validated) before anything was persisted.
	imageParts []llm.MessageContentPart
}

// turnPlan is the assembled prompt and the routing decisions the assistant
// loop runs with.
type turnPlan struct {
	history          []llm.Message
	imageRoute       imageRouting
	gate             toolGate
	editSource       *editImageSource
	knowledgeSources []citation
	// sourceCount is how many [n] markers the attached and knowledge documents
	// took, so web sources continue the numbering.
	sourceCount int
}

// prepareTurn classifies the turn, gates the tools, gathers every context
// block (user, project, attached documents, project knowledge, RAG) and
// builds the model history. It emits the knowledge_sources event as a side
// effect, since the sources are known here and the client wants them before
// the first token.
func (t *turnRun) prepareTurn(in turnInput) turnPlan {
	imageParts := in.imageParts
	// category drives the prompt-classifier block injected below. On the first
	// message we classify now (before the answer history is built) and use the
	// fresh result; on later turns we reuse the stored category.
	//
	// The condition is this being the thread's first turn AND its category never
	// having been set. It used to be shouldGenerateThreadTitle, a proxy for "first
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
	freshlyClassified := len(in.priorMessages) == 0 && strings.TrimSpace(category) == ""

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
		knowledgeSources                  []citation
		sandboxOn                         = t.s.sandboxOffered()
	)
	parallel(
		func() {
			imageRoute = t.s.classifyImageTurn(in.streamCtx, t.user, t.thread.ID, in.content, len(in.imageAttachmentIDs) > 0, in.priorMessages)
		},
		func() {
			if freshlyClassified {
				classified = t.s.classifyFirstTurn(in.streamCtx, t.user, t.thread.ID, t.userMessage.Content)
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
			driftInference := llm.InferenceMetadata{UserID: t.user.ID, Username: t.user.Username, ThreadID: t.thread.ID, Purpose: "classify_drift", Round: 1}
			driftCtx, cancelDrift := context.WithTimeout(in.streamCtx, turnGateTimeout)
			defer cancelDrift()
			turnCategory, _ = t.s.llm.ClassifyThread(llm.WithInferenceMetadata(driftCtx, driftInference), t.userMessage.Content)
		},
		func() { userContext = t.s.memory.UserContext(in.reqCtx, t.user.ID) },
		func() { projectContext = t.s.memory.ProjectContext(in.reqCtx, t.user.ID, t.thread) },
		// run_python's guidance travels with the tool: when the sidecar is
		// off, the prompt never mentions it.
		func() {
			if sandboxOn {
				sandboxGuidance = t.s.sandboxGuidance(in.reqCtx, t.user.ID, t.thread, in.documentAttachmentIDs)
			}
		},
		func() {
			var inlinedDocIDs, knowledgeInlinedIDs map[string]bool
			var attachmentSources []citation
			var inlinedAll bool
			documentContext, inlinedDocIDs, attachmentSources = t.s.documentInlineContext(in.turnCtx, t.user.ID, t.thread, in.documentAttachmentIDs, docIdx)
			knowledgeContext, knowledgeInlinedIDs, knowledgeSources, inlinedAll = t.s.knowledgeInlineContext(in.turnCtx, t.user.ID, t.thread, inlinedDocIDs, docIdx)
			if !inlinedAll {
				ragExclude := mergeDocIDSets(inlinedDocIDs, knowledgeInlinedIDs)
				ragContext, ragSources := t.s.knowledgeContextForThread(in.turnCtx, t.user.ID, t.thread, t.userMessage.Content, ragExclude, docIdx)
				knowledgeContext = joinNonEmptyBlocks(knowledgeContext, ragContext)
				knowledgeSources = append(knowledgeSources, ragSources...)
			}
			knowledgeSources = append(append([]citation(nil), attachmentSources...), knowledgeSources...)
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
		t.s.persistThreadCategory(context.WithoutCancel(in.reqCtx), t.user, t.thread.ID, category)
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
	}, in.priorMessages, t.userMessage)
	// editSourceID is the image whose original pixels are forwarded to the image
	// model for direct editing (image-to-image). Defaults to the photo the user
	// attached this turn; the follow-up branch below sets it to a reused prior image.
	editSourceID := ""
	if len(in.imageAttachmentIDs) > 0 {
		editSourceID = in.imageAttachmentIDs[0]
	}
	// Silently reuse the conversation's most recent image as the model's vision
	// input when this turn is a follow-up edit/restyle ("make it cyberpunk",
	// "create a variation") and the user attached nothing explicitly — so editing
	// the just-generated image needs no manual re-attach step. Fresh creation
	// requests never pull in a prior image. This is best-effort: if the source
	// can't be loaded the turn proceeds text-only rather than failing, unlike an
	// explicit attachment (handled above) whose failure is surfaced to the user.
	if len(imageParts) == 0 && imageRoute.reuseSource {
		if sourceID := latestImageArtifactID(in.priorMessages); sourceID != "" {
			if parts, partsErr := t.s.imageContentParts(in.reqCtx, t.user.ID, t.userMessage.Content, []string{sourceID}); partsErr != nil {
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
		if src, ok, srcErr := t.s.loadEditSourceImage(in.reqCtx, t.user.ID, editSourceID); srcErr != nil {
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
