package httpapi

import (
	"context"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

// turnInput is what the prompt-assembly phase of a persisted turn works from.
type turnInput struct {
	// streamCtx is the turn's cancellable context (usage accumulator attached);
	// turnCtx carries the same attribution on the request context for the
	// helpers that must not be cancelled by a stop; reqCtx is the bare request
	// context.
	streamCtx     context.Context
	turnCtx       context.Context
	reqCtx        context.Context
	stream        *sse.Writer
	user          auth.User
	thread        chat.Thread
	body          streamMessageRequest
	priorMessages []chat.Message
	userMessage   chat.Message
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
func (s *server) prepareTurn(in turnInput) turnPlan {
	imageParts := in.imageParts
	// Decide how this turn routes to image generation/editing before classifying,
	// via one semantic gate call (language-agnostic; see classifyImageTurn). When
	// the image path will run we stamp the in.thread's category deterministically as
	// image_generation instead of letting the text classifier guess (it would
	// mislabel and the image path discards the classifier block anyway). Computed
	// once here and reused below.
	imageRoute := s.classifyImageTurn(in.streamCtx, in.user, in.thread.ID, in.body.Content, len(in.body.ImageAttachmentIDs) > 0, in.priorMessages)
	imageArtifactRequired := imageRoute.generate

	// category drives the prompt-classifier block injected below. On the first
	// message we classify now (synchronously, before the answer history is built)
	// and use the fresh result; on later turns we reuse the stored category.
	//
	// The condition is this being the in.thread's first turn AND its category never
	// having been set. It used to be shouldGenerateThreadTitle, a proxy for "first
	// turn" that held only because the UI creates threads titled with the raw
	// first message — and that leaked: a later turn whose text matched the stored
	// title re-ran the classifier and overwrote the label. CreateThread never sets
	// category, so an empty one is the honest "never classified" signal.
	//
	// Both halves are needed. Without the message check, every pre-existing in.thread
	// with an unset category (there is no backfill migration) would classify on
	// its next turn and stamp that turn's text as the in.thread's sticky identity —
	// on a long in.thread that label is likely wrong, and freshlyClassified would
	// suppress the per-turn drift re-classification below on the same turn. A
	// in.thread's category describes what it opened with, so it is set on turn one or
	// not at all; later drift is handled per-turn just below. Titling has moved
	// after the answer and no longer shares this gate.
	category := in.thread.Category
	freshlyClassified := false
	if len(in.priorMessages) == 0 && strings.TrimSpace(category) == "" {
		categoryOverride := ""
		if imageArtifactRequired {
			categoryOverride = string(classifier.ImageGeneration)
		}
		category = s.classifyThreadForTurn(in.streamCtx, context.WithoutCancel(in.reqCtx), in.user, in.thread.ID, in.userMessage.Content, categoryOverride)
		freshlyClassified = true
	}

	// Semantic drift detection: on a continued turn whose sticky category does not
	// already grant the coding-doc tools, re-classify THIS message so a in.thread that
	// drifted into coding/how-to (in any language) still gets context7 et al. This
	// reuses the same model classifier as the first message — no hand-maintained
	// keyword lexicon. Skipped when the turn was just classified, when the image
	// path will run (its tools are forced), or when the sticky category already
	// grants those tools. Fail-safe: ClassifyThread returns General on failure, so
	// a failed classification simply adds nothing.
	turnCategory := ""
	if !freshlyClassified && !imageArtifactRequired && !categoryGrantsCodingDocs(category) {
		driftInference := llm.InferenceMetadata{UserID: in.user.ID, Username: in.user.Username, ThreadID: in.thread.ID, Purpose: "classify_drift", Round: 1}
		// Bounded like the image gate: the answer waits on this, and General is a
		// fine answer when the endpoint is slow (see turnGateTimeout).
		driftCtx, cancelDrift := context.WithTimeout(in.streamCtx, turnGateTimeout)
		turnCategory, _ = s.llm.ClassifyThread(llm.WithInferenceMetadata(driftCtx, driftInference), in.userMessage.Content)
		cancelDrift()
	}

	// Gate the injected tool set (and the tool guidance that must match it) on the
	// sticky + per-turn category plus explicit-format escalation. Built once here so
	// the system prompt and the offered tools stay consistent (see toolGate).
	gate := newToolGate(category, turnCategory, in.userMessage.Content)
	fileToolGuidance := ""
	if gate.docgenEnabled() {
		fileToolGuidance = fileToolGuardrailPrompt
	}

	userContext := s.userContextForUser(in.reqCtx, in.user.ID)
	projectContext := s.projectContextForThread(in.reqCtx, in.user.ID, in.thread)
	// Inline the full text of any documents attached to this message, and exclude
	// those documents from RAG retrieval below so the model never sees them twice.
	docIdx := newDocIndexer()
	documentContext, inlinedDocIDs, attachmentSources := s.documentInlineContext(in.turnCtx, in.user.ID, in.thread, in.body.DocumentAttachmentIDs, docIdx)
	// Adaptively inject the project's indexed knowledge in full when it fits the
	// token budget (skipping RAG entirely in that case); otherwise fall back to RAG
	// excerpts for whatever did not fit. Auto-inlined documents are excluded from RAG.
	// One numbering space for the whole turn: attachments and knowledge documents
	// take [1]..[k] above, and the web-source registry is seeded to continue at k+1
	// (see runAssistantLoop), so a marker in the answer is unambiguous whatever kind
	// of source it points at.
	knowledgeInlineContext, knowledgeInlinedIDs, knowledgeSources, inlinedAll := s.knowledgeInlineContext(in.turnCtx, in.user.ID, in.thread, inlinedDocIDs, docIdx)
	knowledgeContext := knowledgeInlineContext
	if !inlinedAll {
		ragExclude := mergeDocIDSets(inlinedDocIDs, knowledgeInlinedIDs)
		ragContext, ragSources := s.knowledgeContextForThread(in.turnCtx, in.user.ID, in.thread, in.userMessage.Content, ragExclude, docIdx)
		knowledgeContext = joinNonEmptyBlocks(knowledgeInlineContext, ragContext)
		knowledgeSources = append(knowledgeSources, ragSources...)
	}
	// Attachment citations lead: they were numbered first.
	knowledgeSources = append(append([]citation(nil), attachmentSources...), knowledgeSources...)
	if len(knowledgeSources) > 0 {
		_ = sendSSEJSON(in.stream, "knowledge_sources", map[string]any{"sources": knowledgeSources})
	}
	history := buildLLMHistory(in.user, fileToolGuidance, classifier.Block(category), userContext, projectContext, knowledgeContext, documentContext, in.priorMessages, in.userMessage)
	// editSourceID is the image whose original pixels are forwarded to the image
	// model for direct editing (image-to-image). Defaults to the photo the in.user
	// attached this turn; the follow-up branch below sets it to a reused prior image.
	editSourceID := ""
	if len(in.body.ImageAttachmentIDs) > 0 {
		editSourceID = in.body.ImageAttachmentIDs[0]
	}
	// Silently reuse the conversation's most recent image as the model's vision
	// input when this turn is a follow-up edit/restyle ("make it cyberpunk",
	// "create a variation") and the in.user attached nothing explicitly — so editing
	// the just-generated image needs no manual re-attach step. Fresh creation
	// requests never pull in a prior image. This is best-effort: if the source
	// can't be loaded the turn proceeds text-only rather than failing, unlike an
	// explicit attachment (handled above) whose failure is surfaced to the in.user.
	if len(imageParts) == 0 && imageRoute.reuseSource {
		if sourceID := latestImageArtifactID(in.priorMessages); sourceID != "" {
			if parts, partsErr := s.imageContentParts(in.reqCtx, in.user.ID, in.userMessage.Content, []string{sourceID}); partsErr != nil {
				slog.Warn("auto-attach of prior image failed; continuing without source image",
					"thread_id", in.thread.ID, "artifact_id", sourceID, "err", partsErr)
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
		if src, ok, srcErr := s.loadEditSourceImage(in.reqCtx, in.user.ID, editSourceID); srcErr != nil {
			slog.Warn("load edit source image failed; generating without source image",
				"thread_id", in.thread.ID, "artifact_id", editSourceID, "err", srcErr)
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
