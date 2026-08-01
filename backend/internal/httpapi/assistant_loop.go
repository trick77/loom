package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

const (
	// maxToolRounds caps how many times the model may call tools before loom
	// forces a tool-free final answer. Kept moderate: a model that over-researches
	// (e.g. fetching source after source) otherwise burns rounds — and wall-clock —
	// without converging. Enough for genuine multi-step research, low enough to stop
	// a spiral.
	maxToolRounds        = 6
	maxToolCallsPerRound = 8 // default cap for how many times one tool may run in a single round
	// cheapToolCallsPerRound is the higher per-round cap for the inexpensive
	// fetch/obscura tools: a single paste can carry a dozen links, and fetching
	// them is essentially free, so they should not share the conservative default.
	cheapToolCallsPerRound    = 12
	maxToolCallDuration       = 30 * time.Second
	maxToolResultContentBytes = 32 << 10
	toolFailedPrefix          = "tool failed"
)

// toolCallCapPerRound reports how many times a given tool may run in one round.
// fetch/obscura are very inexpensive (an HTTP read / a headless page load), so
// they get a higher cap than the conservative default that guards pricier tools.
func toolCallCapPerRound(name string) int {
	switch name {
	case fetchToolName, obscuraNavigateToolName, obscuraSnapshotToolName:
		return cheapToolCallsPerRound
	default:
		return maxToolCallsPerRound
	}
}

type assistantLoopResult struct {
	llm.StreamResult
	Artifacts     []artifactResponse
	ToolError     string
	ActivityTrace []activityTraceEvent
	Blocks        []contentBlock
	// WebSources are the web-search/fetch sources gathered this turn, in the [n]
	// order the model cites them. Persisted as citations and rendered as inline
	// source pills + a bottom "Sources" row.
	WebSources []webSource
}

func (s *server) runAssistantLoop(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread, gate toolGate, imageArtifactRequired bool, editSource *editImageSource, typography bool, sourceIndexOffset int) (out assistantLoopResult, outErr error) {
	tools := s.availableTools(thread, gate)
	if len(tools) == 0 {
		b := &blockBuilder{}
		result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), history, inferenceWithPurpose(inference, "chat", 1), nil)
		b.addResult(titles, result)
		if persistInterruptedPartial(result, err) {
			return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
		}
		return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
	}
	if imageArtifactRequired {
		if imageTool := findGenerateImageTool(tools); imageTool != nil {
			return s.runRequiredImageAssistantLoop(ctx, stream, titles, history, inference, user, thread, *imageTool, editSource, typography)
		}
		slog.Warn("image artifact required but generate_image tool is unavailable", "thread_id", thread.ID, "tools", len(tools))
	}

	toolRan := false
	// One generated image per turn, regardless of format: the model sometimes
	// emits several generate_image calls (across rounds or within one round).
	// Only the first that produces an artifact runs; the rest are skipped with a
	// tool result so the model sees the limit and stops asking.
	imageGenerated := false
	// lastRoundDeferred records whether the round that actually ran tools last had
	// to defer any call past its per-round cap. If the loop then exits because the
	// round budget is exhausted, the forced final answer flags the leftover work so
	// the user can ask to continue (a fresh turn has a fresh round budget).
	lastRoundDeferred := false
	var artifacts []artifactResponse
	// reg accumulates web-search/fetch sources across rounds, assigning each a
	// stable [n] index the model cites inline. The sources are persisted with the
	// message (settled render); no live streaming event is emitted.
	reg := newWebSourceRegistryAfter(sourceIndexOffset)
	// Stamp gathered sources onto every subsequent return (natural answer, forced
	// final, interrupted partial) in one place. The tool-less/image fast paths
	// return above this and never gather web sources.
	defer func() { out.WebSources = reg.all() }()
	b := &blockBuilder{}
	// The prompt prefix before any tool round: original system prompt + prior
	// conversation + the user's question. The forced final answer rebuilds a clean
	// synthesis history from this prefix (dropping the tool-call rounds), so capture
	// its length now — the loop only appends, so history[:initialHistoryLen] stays
	// this prefix.
	initialHistoryLen := len(history)
	for round := 1; round <= maxToolRounds; round++ {
		result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), history, inferenceWithPurpose(inference, "chat_tool_round", round), tools)
		if err != nil {
			if persistInterruptedPartial(result, err) {
				b.addResult(titles, result)
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
			}
			return assistantLoopResult{}, err
		}
		b.addResult(titles, result)
		if len(result.ToolCalls) == 0 {
			// A normal textual answer ends the loop. But if the model stops
			// after running tools without producing any text, fall through to a
			// forced, tool-free final answer instead of returning an empty (and
			// therefore discarded) response.
			if strings.TrimSpace(result.Content) != "" || !toolRan {
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
			}
			slog.Info("forcing final answer", "reason", "empty_after_tools", "round", round)
			break
		}
		// Log every tool call's argument size so document payloads are measurable in
		// retrospect instead of guessed at: a create_*_file call serializes the whole
		// file into its argument JSON, so arg_bytes ≈ document size. Pair this with
		// completion_tokens from the matching "llm inference completed" line (same
		// thread_id + round) to read the size in tokens — the unit the completion-token
		// cap is set in. finish_reason=length means the argument was truncated, so
		// arg_bytes is then a lower bound on the intended size. Fires before the length
		// guard below so a truncated payload is still measured.
		for _, call := range result.ToolCalls {
			slog.Info("tool call arguments",
				"round", round,
				"tool", call.Function.Name,
				"arg_bytes", len(call.Function.Arguments),
				"finish_reason", result.FinishReason)
		}
		if result.FinishReason == "length" {
			// The model serializes a document as a single tool-call argument; once it
			// runs past the completion-token cap the argument JSON is truncated
			// mid-string. Appending that broken call to history and continuing makes
			// the upstream reject the next round's prefill (surfacing as a generic
			// "stream failed"), and the document tool itself cannot parse the partial
			// arguments. Stop here with a clear cause instead of replaying it.
			slog.Warn("tool call truncated at token cap",
				"round", round, "tool_calls", len(result.ToolCalls), "finish_reason", result.FinishReason)
			return assistantLoopResult{}, streamUserError{message: "The response was cut off before it finished — the requested output is too large to generate in one turn. Ask for a shorter version or split it into parts."}
		}
		slog.Info("assistant requested tools", "round", round, "tool_calls", len(result.ToolCalls), "content_bytes", len(result.Content))

		history = append(history, llm.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})
		// Per-round, per-tool overflow: a model may batch more calls for one tool
		// than its cap allows in a single round (e.g. a paste with a dozen links →
		// a dozen fetch calls). Rather than aborting the whole turn, run up to the
		// cap and defer the rest with a tool result telling the model to reissue
		// them — the next round has a fresh cap, so the leftovers are picked up
		// automatically within the round budget.
		perToolCount := map[string]int{}
		lastRoundDeferred = false
		for _, call := range result.ToolCalls {
			var output string
			perToolCount[call.Function.Name]++
			// The one-image-per-turn skip is checked first: generate_image is bounded
			// by imageGenerated, not the per-round cap, so it must never fall through
			// to the "reissue it next round" deferral message (which would be wrong —
			// a reissued image call is only skipped again).
			if call.Function.Name == "generate_image" && imageGenerated {
				output = "An image was already generated this turn. Only one image can be generated per turn, so this request was skipped."
			} else if cap := toolCallCapPerRound(call.Function.Name); perToolCount[call.Function.Name] > cap {
				// The instruction rides with the deferral in history so the model sees
				// it on every exit path — including when it concludes with prose without
				// reissuing (which never reaches the forced-final directive below).
				output = fmt.Sprintf("Deferred: at most %d %s call(s) run per round, so this call was not run. Reissue it in a later round to process it. If you finish answering before it runs, tell the user that not everything was processed and offer to continue.", cap, call.Function.Name)
				lastRoundDeferred = true
			} else {
				var response *artifactResponse
				var handled bool
				output, response, handled = s.executeBuiltInTool(ctx, stream, user, thread, call, editSource, typography)
				if handled {
					if response != nil {
						artifacts = append(artifacts, *response)
						b.addArtifact(*response)
						if call.Function.Name == "generate_image" {
							imageGenerated = true
						}
					}
				} else {
					output = s.executeToolCall(ctx, user, call, round, reg)
				}
			}
			if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				return assistantLoopResult{}, err
			}
			b.setToolResult(call.ID, output)
			history = append(history, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    output,
			})
		}
		// Push the sources gathered so far so the browser can resolve [n] markers
		// while the next round's answer streams, instead of waiting for the settled
		// message. A full snapshot, not a delta: idempotent, and the frontend just
		// replaces its list.
		//
		// Emitted every round rather than only when reg.len() grows — addDetailed
		// backfills an already-registered URL's title/snippet/favicon without changing
		// the length, so a page first seen bare via fetch and later enriched by Tavily
		// would otherwise keep showing degraded sidebar data.
		//
		// Ordering is what makes this correct: the model can only cite [n] after
		// seeing it in a tool result, the registry assigns that index while processing
		// the result (above), and sse.Writer.Send is sequential — so the snapshot
		// always reaches the browser before the deltas that reference it.
		if reg.len() > 0 {
			if err := sendSSEJSON(stream, "web_sources", webSourcesResponse{Sources: webSourceCitations(reg.all())}); err != nil {
				return assistantLoopResult{}, err
			}
		}
		toolRan = true
		if round == maxToolRounds {
			slog.Info("forcing final answer", "reason", "rounds_exhausted", "round", round)
		}
	}
	// Force a final answer. Appending a directive to the tool-saturated history does
	// not work: after a research turn MiMo reflexively emits another (unrunnable) tool
	// call — it is pattern-continuing the tool-call/tool-result rounds — which is
	// stripped to empty and dead-ends the turn. Instead rebuild the final turn as a
	// clean, tool-free synthesis over the gathered notes: the shape every reliable
	// prose call in the codebase uses. This also streams the answer live (no tool
	// suppression), so a long synthesis never looks like a dead thread.
	var extraDirective string
	if lastRoundDeferred {
		// Some tool calls were deferred past this turn's per-round caps and never ran.
		// Tell the user so they can ask to continue — a fresh turn gets a fresh round
		// budget. Kept language-neutral so the model answers in the user's language.
		extraDirective = "Some requested items could not be processed within this turn's tool limit; briefly tell the user that not everything was processed and offer to continue if they'd like the rest."
	}
	finalHistory, ok := buildFinalSynthesisHistory(history, initialHistoryLen, extraDirective, reg.all())
	if !ok {
		// No gathered notes (should not happen once tools ran) — fall back to the full
		// history with a plain, tool-free directive.
		directive := "Provide your final answer now using the information already gathered above. Do not call any more tools."
		if extraDirective != "" {
			directive += " " + extraDirective
		}
		finalHistory = append(history[:len(history):len(history)], llm.Message{Role: "system", Content: directive})
	}
	result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), finalHistory, finalAnswerInference(inference, "chat_final", maxToolRounds+1), nil)
	b.addResult(titles, result)
	if persistInterruptedPartial(result, err) {
		return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}
	// Backstop: if the clean synthesis still produced no prose (e.g. the model emitted
	// an inline tool call that was stripped), retry once with a firmer directive, then
	// fall back to a fixed message — anything but persisting an empty turn.
	if err == nil && strings.TrimSpace(result.Content) == "" {
		slog.Info("retrying empty final answer", "reason", "empty_synthesis", "round", maxToolRounds+1)
		retryHistory, ok := buildFinalSynthesisHistory(history, initialHistoryLen, "Answer in plain prose now. Do not emit any tool call or any tool-call markup.", reg.all())
		if !ok {
			retryHistory = append(history[:len(history):len(history)], llm.Message{Role: "system", Content: "Answer the user's question now in plain prose, using only the information already gathered above. Do not emit any tool call."})
		}
		result, err = s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), retryHistory, finalAnswerInference(inference, "chat_final_retry", maxToolRounds+2), nil)
		b.addResult(titles, result)
		if persistInterruptedPartial(result, err) {
			return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
		}
		if err == nil && strings.TrimSpace(result.Content) == "" {
			slog.Warn("final answer empty after retry; using fallback", "round", maxToolRounds+2)
			result.Content = finalAnswerFallback
			// addResult skipped the empty turn's (blank) text; surface the fallback
			// prose as the final text block so the timeline matches the persisted
			// content column.
			b.addText(result.Content)
		}
	}
	return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
}

// finalAnswerFallback is surfaced when the model never commits to a prose answer on
// the forced final turn (it keeps emitting tool calls instead), so the turn shows a
// clear message rather than an empty bubble.
const finalAnswerFallback = "I couldn't put together a final answer from the information gathered. Please try rephrasing or narrowing your question."

func (s *server) runRequiredImageAssistantLoop(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread, imageTool llm.Tool, editSource *editImageSource, typography bool) (assistantLoopResult, error) {
	compilerPrompt := imagePromptCompilerSystemPrompt
	if editSource != nil && len(editSource.Data) > 0 {
		// The source image is forwarded to the model directly, so the compiler must
		// write a concise editing instruction describing only the desired
		// transformation — re-describing the scene would reintroduce the detail loss
		// this path exists to avoid.
		compilerPrompt = imageEditPromptCompilerSystemPrompt
	}
	compilerHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: compilerPrompt,
	})
	b := &blockBuilder{}
	result, err := s.streamAssistantTurnSuppressingContent(ctx, stream, titles, b.nextReasoningID(), compilerHistory, inferenceWithPurpose(inference, "image_prompt_compiler", 1), []llm.Tool{imageTool})
	// The compiler turn's content is deliberately suppressed (it is the internal
	// prompt-compiler output, never shown), so add only its reasoning/tool events
	// — adding its prose would leak hidden text into the timeline.
	b.addTraceOnlyResult(titles, result)
	if err != nil {
		return assistantLoopResult{}, err
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Name != "generate_image" {
		return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}

	call := result.ToolCalls[0]
	history = append(compilerHistory, llm.Message{
		Role:      "assistant",
		ToolCalls: result.ToolCalls,
	})
	output, response, handled := s.executeBuiltInTool(ctx, stream, user, thread, call, editSource, typography)
	if !handled {
		output = capToolOutput("tool failed: generate_image is not available")
	}
	if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
		return assistantLoopResult{}, err
	}
	b.setToolResult(call.ID, output)
	history = append(history, llm.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Content:    output,
	})
	if response == nil {
		return assistantLoopResult{StreamResult: result, ToolError: output, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}

	b.addArtifact(*response)
	artifacts := []artifactResponse{*response}
	finalHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: "Provide a brief final response that refers to the created artifact. Do not call any more tools. Never claim an image was created unless the tool result confirms an artifact.",
	})
	final, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), finalHistory, inferenceWithPurpose(inference, "image_final", 2), nil)
	b.addResult(titles, final)
	if persistInterruptedPartial(final, err) {
		return assistantLoopResult{StreamResult: final, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}
	if err == nil && strings.TrimSpace(final.Content) == "" {
		final.Content = fallbackImageArtifactResponse(*response)
		// addResult skipped the empty final turn's text; surface the fallback prose
		// so the timeline matches the persisted content column.
		b.addText(final.Content)
	}
	return assistantLoopResult{StreamResult: final, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
}

func fallbackImageArtifactResponse(response artifactResponse) string {
	if strings.TrimSpace(response.DisplayFilename) == "" {
		return "Created the image artifact."
	}
	return "Created " + response.DisplayFilename + "."
}

// persistInterruptedPartial reports whether a turn that ended in an interruption —
// a client disconnect (context.Canceled) or a stalled upstream
// (llm.ErrStreamStalled) — still produced partial content worth keeping. Without
// this, a stall after some content streamed would discard the whole turn.
// Reasoning-only output is not persistable on its own.
func persistInterruptedPartial(result llm.StreamResult, err error) bool {
	if strings.TrimSpace(result.Content) == "" {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, llm.ErrStreamStalled)
}

// runIncognitoAssistantTurn runs a single, tool-free assistant turn for an
// ephemeral incognito thread. It mirrors runAssistantLoop's len(tools)==0 fast
// path exactly: with no tools there are no persistence-capable side effects (no
// artifacts, no directive/memory writes), which is what lets an incognito turn
// answer while writing nothing.
func (s *server) runIncognitoAssistantTurn(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata) (assistantLoopResult, error) {
	b := &blockBuilder{}
	result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), history, inferenceWithPurpose(inference, "chat", 1), nil)
	// Safety net: a tool-eager model (MiMo) may still emit an inline tool call
	// despite the no-tool prompt. The parser strips that markup — whether it
	// recovers a call or the markup is truncated/malformed and none is recovered —
	// leaving empty content. Since there are no tools to run, nudge it once to answer
	// directly rather than returning the empty (and therefore discarded) reply. Gated
	// on empty content alone, matching runAssistantLoop's forced-final-answer.
	if err == nil && strings.TrimSpace(result.Content) == "" {
		slog.Info("incognito turn produced no answer text; retrying tool-free", "recovered_tool_calls", len(result.ToolCalls))
		retryHistory := append(append([]llm.Message(nil), history...), llm.Message{Role: "user", Content: incognitoDirectAnswerNudge})
		if retryResult, retryErr := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), retryHistory, inferenceWithPurpose(inference, "chat", 2), nil); retryErr == nil && strings.TrimSpace(retryResult.Content) != "" {
			result = retryResult
		}
	}
	b.addResult(titles, result)
	if persistInterruptedPartial(result, err) {
		return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}
	return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
}

// streamAssistantTurn runs one model turn, relaying reasoning/content deltas and
// tool-call events to the SSE stream. titles/reasoningID let it spawn the
// reasoning abstract the instant the model stops reasoning and starts answering
// (or calling a tool), so the title overlaps the answer stream instead of
// waiting for the turn to finish.
func (s *server) streamAssistantTurn(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	return s.streamAssistantTurnWithContentStreaming(ctx, stream, titles, reasoningID, history, meta, tools, true)
}

func (s *server) streamAssistantTurnSuppressingContent(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	return s.streamAssistantTurnWithContentStreaming(ctx, stream, titles, reasoningID, history, meta, tools, false)
}

func (s *server) streamAssistantTurnWithContentStreaming(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool, streamContent bool) (llm.StreamResult, error) {
	callCtx := llm.WithInferenceMetadata(ctx, meta)
	var reasoningBuf strings.Builder
	titleSpawned := false
	// The reasoning->content (or reasoning->tool) boundary: the model has
	// finished thinking, so the round's reasoning is complete and its title can
	// generate while the answer streams.
	spawnTitle := func() {
		if titleSpawned {
			return
		}
		titleSpawned = true
		titles.spawn(reasoningID, reasoningBuf.String())
	}
	return s.llm.StreamChatWithTools(callCtx, history, tools, func(event llm.StreamEvent) error {
		if event.ReasoningDelta != "" {
			reasoningBuf.WriteString(event.ReasoningDelta)
			return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
		}
		if event.ToolPending {
			spawnTitle()
			return sendSSEJSON(stream, "tool_pending", struct{}{})
		}
		if event.Delta != "" && streamContent {
			spawnTitle()
			return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
		}
		if event.ToolCall.ID != "" || event.ToolCall.Function.Name != "" {
			spawnTitle()
			return sendSSEJSON(stream, "tool_call", toolCallResponse{
				ID:        event.ToolCall.ID,
				Name:      event.ToolCall.Function.Name,
				Arguments: event.ToolCall.Function.Arguments,
			})
		}
		return nil
	})
}

func inferenceWithPurpose(metadata llm.InferenceMetadata, purpose string, round int) llm.InferenceMetadata {
	metadata.Purpose = purpose
	metadata.Round = round
	return metadata
}

// finalAnswerMaxCompletionTokens is the widened completion budget for the forced
// final answer. The default chat cap is sized for a single answer with thinking;
// the forced final synthesizes many gathered sources with thinking off, so it
// gets more room for prose (and never spends the budget on reasoning).
const finalAnswerMaxCompletionTokens = 8192

// finalAnswerInference builds the metadata for a forced final-answer turn: it
// disables thinking and widens the completion budget. By this point all research
// reasoning already happened across the tool rounds and is in history, so the
// model only needs to write the answer — leaving thinking on lets a reasoning
// model burn the whole budget thinking and emit no prose (finish_reason=length),
// which is the failure this turns off.
func finalAnswerInference(metadata llm.InferenceMetadata, purpose string, round int) llm.InferenceMetadata {
	metadata = inferenceWithPurpose(metadata, purpose, round)
	metadata.SuppressThinking = true
	metadata.MaxCompletionTokens = finalAnswerMaxCompletionTokens
	return metadata
}
