package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

const loomSystemPrompt = "Default to flowing prose — full sentences grouped into paragraphs — when explaining or describing something. Reach for markdown structure only when it genuinely helps the reader: a list when the content is a true enumeration the user would naturally keep as a list (steps to follow, distinct parameters, a checklist), a table to compare several items across the same dimensions, and headings only for genuinely long, multi-section answers. Keep short or simple answers as plain prose — do not add structure for its own sake. Use **bold** sparingly to mark key terms. Put code in fenced markdown blocks. When unsure, use available tools to find the answer before responding; if they turn up nothing, say you don't know rather than guessing. When the user refers to an earlier discussion or decision, or before answering a question that your past conversations together likely already covered, call conversation_search to find the relevant prior threads, then read_thread with a result's thread id to read one in full. Once the tool results give you enough to answer, stop and respond — do not keep fetching more sources past what the request needs. If you are about to say a topic is beyond your knowledge, too recent, or past your training cutoff, first use the available search and fetch tools to look it up; only say you don't know after those tools return nothing useful. For image or logo generation, editing, restyling, or variation requests, call the image generation tool before answering. Never claim that an image was generated unless an image artifact was actually created. The generated image is shown to the user automatically as an attachment; never embed, link, or reference it by filename (no markdown `![]()` or `<img>` tags) in your reply. Long code or data you include inline is fine and is offered for download automatically. For URLs, use the lightweight fetch tool first when the task is to read, summarize, quote, or extract page text. Use the browser navigation tool only when fetch cannot access useful content or the page needs JavaScript rendering; it navigates to the URL and reads back the rendered page. Web search results, fetched pages and excerpts from the user's uploaded documents are all labeled with a bracketed number like [1] or [2]. Whenever a sentence or paragraph in your answer draws on one of these sources, append its marker at the end of that sentence — [1], or several like [1][3]. The numbers form one sequence across documents and web sources, so a marker is never ambiguous. Use only numbers that actually appear in the material provided; never invent a citation number, and do not cite anything that was not given to you as a numbered source. Ignore the language of tool results and retrieved documents."

// fileToolGuardrailPrompt steers when to call the file-creation (docgen) tools.
// It is injected into the system prompt only on turns where those tools are
// actually offered (see toolGate.docgenEnabled) — naming create_pdf_file et al.
// when they are gated out of the request would invite the model to call a tool
// whose schema it never received, producing a malformed call. Kept in sync with
// the docgen tool set and docgen.FileToolGuardrail.
const fileToolGuardrailPrompt = "Only call a file-creation tool (create_text_file, create_pdf_file, create_xlsx_file, create_docx_file, create_pptx_presentation) when the user explicitly asks to save, create, export, or download a file; for summarize, explain, or analyze requests — including about attached documents — answer inline in the chat and do not produce a downloadable file."

const imagePromptCompilerSystemPrompt = "The latest user request requires image generation or editing. Your only job is to call `generate_image` exactly once. Do not answer conversationally before the tool call. Do not refuse based on being text-based. Transform the user's request into a concise, visually rich prompt that preserves subject, setting, style, composition, mood, medium, text requirements, and constraints. Add only helpful visual details consistent with the request. Always set `filename` to a short, descriptive name based on the image's main subject (2-4 words, lowercase, hyphen-separated, no path or extension), e.g. `red-fox-in-snow`. Set `aspect_ratio` to the shape the request calls for — wide (16:9, 3:2) for scenery, banners and desktop wallpapers, tall (9:16, 2:3) for posters, book covers and phone wallpapers, 4:3 or 3:4 for a mild lean — and leave it at 1:1 when nothing in the request implies a shape. Infer this from what the user is asking for in whatever language they wrote it in; do not look for particular words. After the tool result, provide a brief final response that refers to the created artifact. The generated image is shown to the user automatically as an attachment; never embed, link, or reference it by filename (no markdown `![]()` or `<img>` tags) in your reply. Never claim an image was created unless the tool result confirms an artifact."

// imageEditPromptCompilerSystemPrompt is used when the user's source image is being
// forwarded to the model directly (image-to-image). The model already sees the
// pixels, so the prompt must describe only the transformation — re-describing the
// scene would reintroduce the detail loss the direct-upload path exists to avoid.
const imageEditPromptCompilerSystemPrompt = "The latest user request edits or transforms an image the user provided, and that source image is supplied to the image model directly. Your only job is to call `generate_image` exactly once. Do not answer conversationally before the tool call. Do not refuse based on being text-based. Write the `prompt` as a concise editing instruction describing ONLY the desired transformation, style, or change to apply to the provided image (e.g. \"Rebuild the provided photo as a detailed LEGO brick set, faithful to its composition and colors\"). Do NOT re-describe the original scene in detail — the model already sees it; restating it discards detail. Always set `filename` to a short, descriptive name based on the result's main subject (2-4 words, lowercase, hyphen-separated, no path or extension), e.g. `lego-city-skyline`. Do not set `aspect_ratio` unless the user asks for a different shape: the edit keeps the source image's proportions by default. After the tool result, provide a brief final response that refers to the created artifact. The generated image is shown to the user automatically as an attachment; never embed, link, or reference it by filename (no markdown `![]()` or `<img>` tags) in your reply. Never claim an image was created unless the tool result confirms an artifact."

var (
	errStreamStopRequested = errors.New("stream stop requested")
	errStreamSuperseded    = errors.New("stream superseded by newer request")
	errStreamThreadDeleted = errors.New("stream canceled: thread deleted")
)

// streamHeartbeatInterval is how often the SSE stream emits a keep-alive comment
// while silent. Kept well under the 60-120s idle timeouts common to proxies, load
// balancers, and edges (e.g. Cloudflare ~100s) so a long silent tool-call
// generation never trips them. See sse.Writer.Heartbeat.
const streamHeartbeatInterval = 20 * time.Second

func (s *server) handleStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	var body streamMessageRequest
	if err := decodeJSONBodyLimit(w, r, &body, maxStreamBodyBytes); err != nil {
		writeDecodeError(w, err)
		return
	}

	threadID := r.PathValue("threadID")
	thread, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	priorMessages, found, err := s.thread.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "list messages failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	// Resolve the attached images into model content parts BEFORE anything is
	// persisted or streamed: a bad attachment list (too many, unknown id, not an
	// image) is a plain 400 here. Once the user message is stored and the SSE
	// stream is open there is no way to reject the send cleanly. The store trims
	// content, so the text part uses the same trimmed form the message will carry.
	imageParts, imageArtifacts, err := s.resolveImageAttachments(r.Context(), user.ID, strings.TrimSpace(body.Content), body.ImageAttachmentIDs)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Persist the images and documents the user sent with this message so the sent
	// previews survive a reload (resolved user-scoped; out-of-scope ids skipped).
	sentAttachments := s.resolveSentAttachments(r.Context(), user.ID, thread, body.ImageAttachmentIDs, body.DocumentAttachmentIDs, imageArtifacts)
	// Persist the collapsed paste blocks so the sent bubble renders "Pasted" chips
	// on reload instead of the inline wall of text. Their text is already folded
	// into body.Content, so the model and every content-derived path (title,
	// classifier, RAG, history) are unchanged; this is render-only metadata.
	pastedTexts := marshalPastedTexts(body.PastedTexts)
	userMessage, err := s.thread.AddMessageWithAttachments(r.Context(), user.ID, threadID, chat.RoleUser, body.Content, sentAttachments, pastedTexts)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	streamCtx, cancelStream := context.WithCancelCause(r.Context())
	defer cancelStream(nil)
	// Sum token usage across every model call this turn makes — answer turns, tool
	// rounds, and the background reasoning/thread-title helpers — so the persisted
	// per-message stats reflect the whole turn, not just the final answer call.
	// turnStart times the full turn wall-clock for the same reason.
	usageTotal := llm.NewUsageAccumulator()
	streamCtx = llm.WithUsageAccumulator(streamCtx, usageTotal)
	// Attribute the whole turn up front, so the model calls that hang off its
	// edges — the RAG query embedding, a live vision description for an attached
	// image, the image tool — log under the same user/thread as the chat calls.
	// The per-call metadata attached below (purposes, rounds, reasoning effort)
	// builds on this; without it those edge calls logged anonymously.
	turnAttribution := llm.InferenceMetadata{
		UserID:   user.ID,
		Username: user.Username,
		ThreadID: threadID,
	}
	streamCtx = llm.WithInferenceMetadata(streamCtx, turnAttribution)
	// The prompt-assembly helpers below run on the request context rather than
	// streamCtx, so they need the same attribution attached separately — and the
	// accumulator, so their calls (the RAG query embedding) count in the turn.
	turnCtx := llm.WithUsageAccumulator(llm.WithInferenceMetadata(r.Context(), turnAttribution), usageTotal)
	turnStart := time.Now()
	unregisterStream := s.activeStreams.register(user.ID, threadID, cancelStream)
	defer unregisterStream()

	stream, err := sse.NewWriter(w)
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "client_message", "sse writer init failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// From here on the 200 is committed, so a panic must end the stream with an
	// error event rather than reach the recovery middleware, which can no longer
	// answer with a 500.
	defer recoverToStream(stream, r)
	// Keep the connection alive through idle proxies during long silent gaps —
	// notably while MiMo serializes a large tool-call argument server-side and
	// streams nothing to the client for up to a few minutes (see sse.Heartbeat).
	defer stream.Heartbeat(streamCtx, streamHeartbeatInterval)()
	// Book what the turn spent on every exit path. Deferred ahead of
	// titles.wait below so it runs after it: the reasoning-title calls must have
	// finished before the turn's cost is read. The success path settles
	// explicitly before "done"; this is then a no-op.
	costs := &turnCostSettler{s: s, user: user, userMessageID: userMessage.ID, acc: usageTotal}
	defer costs.settle(context.WithoutCancel(r.Context()))
	if err := sendSSEJSON(stream, "user_message", userMessage); err != nil {
		return
	}

	plan := s.prepareTurn(turnInput{
		streamCtx:     streamCtx,
		turnCtx:       turnCtx,
		reqCtx:        r.Context(),
		stream:        stream,
		user:          user,
		thread:        thread,
		body:          body,
		priorMessages: priorMessages,
		userMessage:   userMessage,
		imageParts:    imageParts,
	})
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID}
	// Background reasoning-title generation. The deferred wait is a safety net so
	// no title goroutine writes to the SSE stream after the handler returns on an
	// early error path.
	titles := newReasoningTitleTracker(streamCtx, s, stream, inference, userResponseLanguage(user))
	defer titles.wait()
	// titleThread names an as-yet-untitled thread. It runs after the answer so the
	// title model can see the reply, not just the question — passing an empty
	// assistantMessage reproduces the input-only titling this handler did for
	// every turn before, and is what the paths below that never produce an answer
	// use. Detached from the request context so a canceled or failed turn still
	// names the thread, as it did when titling ran up front.
	titleThread := func(assistantMessage string) {
		if !shouldGenerateThreadTitle(thread.Title, userMessage.Content) {
			return
		}
		// The thread is being deleted, so there is nothing left to name: the title
		// call would spend tokens on a model round whose UpdateThread then no-ops on
		// the missing thread. It also runs inline on a detached context, which would
		// hold the deleting request in its bounded wait for several seconds.
		if errors.Is(context.Cause(streamCtx), errStreamThreadDeleted) {
			return
		}
		// Derived from streamCtx, not r.Context(): streamCtx carries the turn's
		// usage accumulator, so the title call's tokens land in both the
		// per-message stats and the lifetime rollup. WithoutCancel keeps that
		// value while letting the call outlive a client disconnect.
		titleCtx := context.WithoutCancel(streamCtx)
		if err := s.generateAndSendThreadTitle(titleCtx, titleCtx, stream, user, threadID, thread.Title, userMessage.Content, assistantMessage); err != nil {
			slog.Warn("thread title generation failed", "thread_id", threadID, "error", err)
		}
	}

	assistantResult, err := s.runAssistantLoop(streamCtx, stream, titles, plan.history, inference, user, thread, plan.gate, plan.imageRoute.generate, plan.editSource, plan.imageRoute.typography, userMessage.Content, plan.sourceCount)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancelSource, cancelReason := streamCancelDetails(streamCtx)
			slog.Info("message stream canceled",
				"thread_id", threadID,
				"cancel_source", cancelSource,
				"reason", cancelReason,
				"content_bytes", len(assistantResult.Content),
				"reasoning_bytes", len(assistantResult.ReasoningContent),
				"tool_calls", len(assistantResult.ToolCalls))
			// Whatever streamed before the cancel is still the best title source
			// available; it is simply shorter than a completed answer.
			titleThread(assistantResult.Content)
			// End the stream deliberately. A client that did not issue the stop
			// itself (the thread open in a second tab) would otherwise read an
			// unterminated stream as a dropped connection. The write fails
			// harmlessly when the client is the one that went away.
			_ = stream.Send("done", "{}")
			return
		}
		message := streamFailureMessage(err, assistantResult, "message", threadID)
		_ = sendSSEJSON(stream, "error", map[string]string{"error": message})
		titleThread(assistantResult.Content)
		return
	}
	assistantContent := assistantResult.Content
	if plan.imageRoute.generate && len(assistantResult.Artifacts) == 0 {
		message := "image generation was not completed"
		if strings.TrimSpace(assistantResult.ToolError) != "" {
			message = assistantResult.ToolError
		}
		slog.Warn("image request completed without image artifact",
			"thread_id", threadID,
			"content_bytes", len(assistantResult.Content),
			"tool_calls", len(assistantResult.ToolCalls),
			"tool_error", assistantResult.ToolError)
		_ = sendSSEJSON(stream, "error", map[string]string{"error": message})
		titleThread(assistantContent)
		return
	}
	if strings.TrimSpace(assistantContent) == "" {
		slog.Warn("empty assistant response",
			"thread_id", threadID,
			"content_bytes", len(assistantResult.Content),
			"reasoning_bytes", len(assistantResult.ReasoningContent),
			"tool_calls", len(assistantResult.ToolCalls))
		// assistantContent is empty here by definition, so this titles from the
		// question alone — exactly what every turn did before the reordering.
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "empty assistant response"})
		titleThread(assistantContent)
		return
	}

	persistCtx := context.WithoutCancel(r.Context())
	assistantMessage, err := s.persistAssistantTurn(persistCtx, stream, titles, user, thread, &assistantResult, plan.knowledgeSources, usageTotal, turnStart)
	if err != nil {
		slog.Warn("persist assistant message failed", "thread_id", threadID, "err", err)
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "persist assistant message failed"})
		titleThread(assistantContent)
		return
	}
	costs.setAssistant(assistantMessage)
	if err := sendSSEJSON(stream, "assistant_message", assistantMessage); err != nil {
		return
	}

	// Name the thread now that the answer exists. This is the whole point of the
	// ordering: the reply supplies the facts, the correct spellings and a strong
	// signal of the language the turn was actually conducted in — a bare question
	// supplies none of that, and the title model used to guess from it alone.
	//
	// Deliberately after the answer is persisted and delivered, not before: this
	// call is bounded by turnGateTimeout, and a slow short-gate endpoint would
	// otherwise hold the just-streamed answer unpersisted — and the UI in its
	// streaming state — for up to that long. The cost is that the title call's
	// tokens miss the per-message stats; its cost is added onto the message and
	// its tokens reach the lifetime rollup when the turn settles below.
	titleThread(assistantContent)

	// Bump the thread to the top of the sidebar live. last_message_at was just
	// updated by the assistant message; the frontend reorders on this event
	// (ThreadShell onThread -> upsertThread). On newly-titled threads a thread event
	// was just sent by titleThread above; re-sending is idempotent (upsertThread
	// moves to top) and now carries the final last_message_at. Best-effort: a
	// failed/not-found fetch skips the event — the DB order is already correct for
	// the next refetch.
	if updated, found, getErr := s.thread.GetThread(persistCtx, user.ID, threadID); getErr == nil && found {
		_ = sendSSEJSON(stream, "thread", updated)
	}

	// Every call of the turn has finished (persistAssistantTurn waited for the
	// reasoning titles, the thread title ran just above): book the spend now,
	// before "done", so a reload right after sees the final Σ.
	costs.settle(persistCtx)

	// Best-effort, debounced background refresh of the project's shared memory so
	// sibling chats stay aware of this turn (at most once per memoryProjectDebounce).
	// Detaches from the request context so it survives the handler returning. This
	// also one-shot fills an empty project description (see refreshMemory). User
	// memory is intentionally NOT refreshed here — it is handled by the once-a-day
	// MemoryWorker sweep so it does not fire on every turn.
	s.maybeRefreshProjectMemoryAsync(r.Context(), user, thread)

	_ = stream.Send("done", "{}")
}

func (s *server) handleStopStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	_, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	s.activeStreams.stop(user.ID, threadID, stopCause(r.URL.Query().Get("source")))
	w.WriteHeader(http.StatusNoContent)
}

// stopCause builds the cancellation cause for an explicit client stop. It always
// wraps errStreamStopRequested (so cancel_source stays "stop_endpoint"), and folds
// the client-declared UI trigger — "stop_button", "escape", "new_send" — into the
// cause message so it surfaces in the canceled log's reason field. Attribution is
// reliable because the client awaits this stop request before aborting its fetch,
// so this cause wins the WithCancelCause race over the raw request-context cancel.
func stopCause(source string) error {
	source = sanitizeCancelSource(source)
	if source == "" {
		return errStreamStopRequested
	}
	return fmt.Errorf("%w (%s)", errStreamStopRequested, source)
}

// sanitizeCancelSource clamps a client-supplied source label to a short
// [a-z0-9_] token so it can be logged verbatim without injection risk.
func sanitizeCancelSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if len(source) > 32 {
		source = source[:32]
	}
	var b strings.Builder
	for _, r := range source {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type streamUserError struct {
	message string
}

func (e streamUserError) Error() string {
	return e.message
}

func sendSSEJSON(stream *sse.Writer, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return stream.Send(event, string(payload))
}

func streamCancelDetails(ctx context.Context) (string, string) {
	cause := context.Cause(ctx)
	if cause == nil {
		return "", ""
	}
	source := "unknown"
	switch {
	case errors.Is(cause, errStreamStopRequested):
		source = "stop_endpoint"
	case errors.Is(cause, errStreamThreadDeleted):
		source = "thread_deleted"
	case errors.Is(cause, errStreamSuperseded):
		source = "superseded_stream"
	case errors.Is(cause, context.Canceled):
		source = "request_context"
	case errors.Is(cause, context.DeadlineExceeded):
		source = "deadline"
	}
	return source, cause.Error()
}

// recoverToStream is deferred by the stream handlers once the SSE response is
// committed. It turns a panic into a terminal error event so the client sees a
// failed turn instead of a stream that just stops. The panic is logged here
// with its stack; it is not re-raised because the recovery middleware could
// only write a 500 into the open stream.
func recoverToStream(stream *sse.Writer, r *http.Request) {
	if p := recover(); p != nil {
		slog.Error("panic recovered mid-stream", "err", p, "path", r.URL.Path, "stack", string(debug.Stack()))
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "internal server error"})
	}
}
