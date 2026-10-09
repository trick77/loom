package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
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
	sandboxToolCallsPerRound  = 3
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
	case sandboxToolName:
		// Each job can hold a sandbox slot for up to a minute; a round that
		// wants more is better split across rounds.
		return sandboxToolCallsPerRound
	default:
		return maxToolCallsPerRound
	}
}

// LoopResult is what the assistant loop produced: the final answer call's
// result plus everything the turn gathered on the way to it.
type LoopResult struct {
	llm.StreamResult
	Artifacts     []ArtifactResponse
	ToolError     string
	ActivityTrace []ActivityTraceEvent
	Blocks        []ContentBlock
	// WebSources are the web-search/fetch sources gathered this turn, in the [n]
	// order the model cites them. Persisted as citations and rendered as inline
	// source pills + a bottom "Sources" row.
	WebSources []webSource
}

// RunAssistantLoop answers the turn from the history Prepare built, running
// tool rounds as the model asks for them, then a forced final answer if the
// rounds run out.
func (t *Run) RunAssistantLoop(ctx context.Context) (out LoopResult, outErr error) {
	history := t.plan.history
	tools := t.e.availableTools(t.thread, t.plan.gate)
	if len(tools) == 0 {
		b := &blockBuilder{}
		result, err := t.streamAssistantTurn(ctx, b.nextReasoningID(), history, inferenceWithPurpose(t.inference, "chat", 1), nil)
		b.addResult(t.titles, result)
		if persistInterruptedPartial(result, err) {
			return b.result(result, nil, ""), nil
		}
		return b.result(result, nil, ""), err
	}
	if t.plan.imageRoute.generate {
		if imageTool := findGenerateImageTool(tools); imageTool != nil {
			return t.runRequiredImageAssistantLoop(ctx, history, *imageTool)
		}
		slog.Warn("image artifact required but generate_image tool is unavailable", "thread_id", t.thread.ID, "tools", len(tools))
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
	var artifacts []ArtifactResponse
	// reg accumulates web-search/fetch sources across rounds, assigning each a
	// stable [n] index the model cites inline, continuing after any documents
	// numbered before the loop. A snapshot is pushed to the browser after every
	// round (see the web_sources event below) so inline markers resolve while the
	// answer streams; the same sources are also persisted with the message.
	reg := newWebSourceRegistryAfter(t.plan.sourceCount)
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
		result, err := t.streamAssistantTurn(ctx, b.nextReasoningID(), history, inferenceWithPurpose(t.inference, "chat_tool_round", round), tools)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, llm.ErrStreamStalled) {
				return LoopResult{}, err
			}
			b.addResult(t.titles, result)
			if b.keepInterrupted(&result, err, artifacts) {
				return b.result(result, artifacts, ""), nil
			}
			return LoopResult{}, err
		}
		b.addResult(t.titles, result)
		if len(result.ToolCalls) == 0 {
			// A normal textual answer ends the loop. But if the model stops
			// after running tools without producing any text, fall through to a
			// forced, tool-free final answer instead of returning an empty (and
			// therefore discarded) response. The same applies when thinking ate
			// the whole completion cap (finish_reason=length, no text): the forced
			// final runs with thinking off, so it cannot run out the same way.
			empty := strings.TrimSpace(result.Content) == ""
			truncated := empty && result.FinishReason == "length"
			if !empty || (!toolRan && !truncated) {
				return b.result(result, artifacts, ""), nil
			}
			reason := "empty_after_tools"
			if truncated {
				reason = "reasoning_hit_cap"
			}
			slog.Info("forcing final answer", "reason", reason, "round", round)
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
			return LoopResult{}, streamUserError{message: "The response was cut off before it finished — the requested output is too large to generate in one turn. Ask for a shorter version or split it into parts."}
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
		deferred := make([]bool, len(result.ToolCalls))
		for i, call := range result.ToolCalls {
			perToolCount[call.Function.Name]++
			deferred[i] = perToolCount[call.Function.Name] > toolCallCapPerRound(call.Function.Name)
		}
		// The round's independent web reads start now and overlap; everything
		// below still handles the calls one at a time, in the model's order.
		runsCtx, cancelRuns := context.WithCancel(ctx)
		runs := t.e.startToolRuns(runsCtx, result.ToolCalls, deferred)
		lastRoundDeferred = false
		for i, call := range result.ToolCalls {
			var output string
			// The one-image-per-turn skip is checked first: generate_image is bounded
			// by imageGenerated, not the per-round cap, so it must never fall through
			// to the "reissue it next round" deferral message (which would be wrong —
			// a reissued image call is only skipped again).
			if call.Function.Name == "generate_image" && imageGenerated {
				output = "An image was already generated this turn. Only one image can be generated per turn, so this request was skipped."
			} else if deferred[i] {
				cap := toolCallCapPerRound(call.Function.Name)
				// The instruction rides with the deferral in history so the model sees
				// it on every exit path — including when it concludes with prose without
				// reissuing (which never reaches the forced-final directive below).
				output = fmt.Sprintf("Deferred: at most %d %s call(s) run per round, so this call was not run. Reissue it in a later round to process it. If you finish answering before it runs, tell the user that not everything was processed and offer to continue.", cap, call.Function.Name)
				lastRoundDeferred = true
			} else {
				var created []ArtifactResponse
				var handled bool
				output, created, handled = t.executeBuiltInTool(ctx, call)
				if handled {
					for _, response := range created {
						artifacts = append(artifacts, response)
						b.addArtifact(response)
					}
					if len(created) > 0 && call.Function.Name == "generate_image" {
						imageGenerated = true
					}
				} else if runs[i] != nil {
					output = t.finishToolCall(ctx, call, round, reg, <-runs[i])
				} else {
					output = t.finishToolCall(ctx, call, round, reg, t.e.runToolCall(ctx, call))
				}
			}
			if err := t.stream.Send("tool_result", ToolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				cancelRuns()
				return LoopResult{}, err
			}
			b.setToolResult(call.ID, output)
			history = append(history, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    output,
			})
		}
		cancelRuns()
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
		// the result (above), and stream.Send is sequential — so the snapshot
		// always reaches the browser before the deltas that reference it.
		if reg.len() > 0 {
			if err := t.stream.Send("web_sources", WebSourcesResponse{Sources: webSourceCitations(reg.all())}); err != nil {
				return LoopResult{}, err
			}
		}
		toolRan = true
		if round == maxToolRounds {
			slog.Info("forcing final answer", "reason", "rounds_exhausted", "round", round)
		}
	}
	// Force a final answer. Appending a directive to the tool-saturated history does
	// not work: after a research turn a model may reflexively emit another (unrunnable) tool
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
	result, err := t.streamAssistantTurn(ctx, b.nextReasoningID(), finalHistory, finalAnswerInference(t.inference, "chat_final", maxToolRounds+1), nil)
	b.addResult(t.titles, result)
	if b.keepInterrupted(&result, err, artifacts) {
		return b.result(result, artifacts, ""), nil
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
		result, err = t.streamAssistantTurn(ctx, b.nextReasoningID(), retryHistory, finalRetryInference(t.inference, result), nil)
		b.addResult(t.titles, result)
		if b.keepInterrupted(&result, err, artifacts) {
			return b.result(result, artifacts, ""), nil
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
	return b.result(result, artifacts, ""), err
}

// finalAnswerFallback is surfaced when the model never commits to a prose answer on
// the forced final turn (it keeps emitting tool calls instead), so the turn shows a
// clear message rather than an empty bubble.
const finalAnswerFallback = "I couldn't put together a final answer from the information gathered. Please try rephrasing or narrowing your question."

// runRequiredImageAssistantLoop answers an image turn: a prompt-compiler round
// that must call imageTool, the image call itself, then a brief final answer.
func (t *Run) runRequiredImageAssistantLoop(ctx context.Context, history []llm.Message, imageTool llm.Tool) (LoopResult, error) {
	compilerPrompt := imagePromptCompilerSystemPrompt
	if editSource := t.plan.editSource; editSource != nil && len(editSource.Data) > 0 {
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
	result, err := t.streamAssistantTurnSuppressingContent(ctx, b.nextReasoningID(), compilerHistory, inferenceWithPurpose(t.inference, "image_prompt_compiler", 1), []llm.Tool{imageTool})
	// The compiler turn's content is deliberately suppressed (it is the internal
	// prompt-compiler output, never shown), so add only its reasoning/tool events
	// — adding its prose would leak hidden text into the timeline.
	b.addTraceOnlyResult(t.titles, result)
	if err != nil {
		return LoopResult{}, err
	}
	// The first generate_image call wins. Only one image is generated per turn, and
	// a compiled call — with its prompt, aspect ratio and filename — beats the raw
	// user text the fallback would otherwise send, so extra calls alongside it are
	// no reason to discard it.
	var call llm.ToolCall
	var compiled bool
	for _, candidate := range result.ToolCalls {
		if candidate.Function.Name == "generate_image" {
			call, compiled = candidate, true
			break
		}
	}
	if !compiled {
		// The user's own text comes from the stored message, not history: the last
		// history message's Content is blanked when image parts are attached.
		fallback, ok := fallbackImageToolCall(t.userMessage.Content)
		if !ok {
			return b.result(result, nil, ""), nil
		}
		slog.Warn("image prompt compiler produced no usable tool call; generating from the user's own text",
			"thread_id", t.thread.ID, "tool_calls", len(result.ToolCalls))
		call = fallback
		// Nothing announced this call: it was synthesized here rather than streamed,
		// so neither the browser nor the trace has seen it. Announce it exactly as a
		// streamed call would be, or the tool_result below refers to a step that
		// does not exist on either side.
		b.addTraceEvent(toolCallEvent(call))
		if err := t.stream.Send("tool_call", ToolCallResponse{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		}); err != nil {
			return LoopResult{}, err
		}
	}
	history = append(compilerHistory, llm.Message{
		Role:      "assistant",
		ToolCalls: []llm.ToolCall{call},
	})
	output, created, handled := t.executeBuiltInTool(ctx, call)
	if !handled {
		output = capToolOutput("tool failed: generate_image is not available")
	}
	if err := t.stream.Send("tool_result", ToolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
		return LoopResult{}, err
	}
	b.setToolResult(call.ID, output)
	history = append(history, llm.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Content:    output,
	})
	if len(created) == 0 {
		return b.result(result, nil, output), nil
	}

	b.addArtifact(created[0])
	artifacts := created[:1]
	finalHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: "Provide a brief final response that refers to the created artifact. Do not call any more tools. Never claim an image was created unless the tool result confirms an artifact.",
	})
	final, err := t.streamAssistantTurn(ctx, b.nextReasoningID(), finalHistory, inferenceWithPurpose(t.inference, "image_final", 2), nil)
	b.addResult(t.titles, final)
	if b.keepInterrupted(&final, err, artifacts) {
		return b.result(final, artifacts, ""), nil
	}
	if err == nil && strings.TrimSpace(final.Content) == "" {
		final.Content = fallbackArtifactResponse(created[0])
		// addResult skipped the empty final turn's text; surface the fallback prose
		// so the timeline matches the persisted content column.
		b.addText(final.Content)
	}
	return b.result(final, artifacts, ""), err
}

func fallbackArtifactResponse(response ArtifactResponse) string {
	if strings.TrimSpace(response.DisplayFilename) == "" {
		return "Created the artifact."
	}
	return "Created " + response.DisplayFilename + "."
}

// fallbackImageToolCall builds the generate_image call the prompt compiler
// should have made, from the user's own message text. The compiler sometimes
// answers an image turn with reasoning and no tool call at all — a refusal on
// content grounds, typically — which would otherwise end the turn showing
// nothing, even though the turn was already routed as an image request and the
// image provider runs its own moderation. Falling back loses the compiler's
// context resolution, translation and framing, so this is a last resort rather
// than a shortcut past it.
//
// Reports false when there is no user text to send, leaving the caller's
// original empty-handed return in place.
func fallbackImageToolCall(userPrompt string) (llm.ToolCall, bool) {
	prompt := strings.TrimSpace(userPrompt)
	if prompt == "" {
		return llm.ToolCall{}, false
	}
	if runes := []rune(prompt); len(runes) > imagegen.MaxPromptRunes {
		prompt = string(runes[:imagegen.MaxPromptRunes])
	}
	// No filename: the provider derives one from the prompt itself, with the
	// character set the artifact store can actually keep.
	args, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		return llm.ToolCall{}, false
	}
	return llm.ToolCall{
		ID:       "fallback_generate_image",
		Type:     "function",
		Function: llm.ToolCallFunction{Name: "generate_image", Arguments: string(args)},
	}, true
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

// keepInterrupted is persistInterruptedPartial for a multi-round turn: a round
// the user stopped before it streamed any prose still keeps what earlier rounds
// produced. Their prose, or else a line naming the last artifact, becomes the
// persisted content, so a stop after a tool created a file does not drop the
// file from the transcript. A stall gets no such fallback: it is the upstream
// failing, and the user has to see that.
func (b *blockBuilder) keepInterrupted(result *llm.StreamResult, err error, artifacts []ArtifactResponse) bool {
	if strings.TrimSpace(result.Content) != "" || !errors.Is(err, context.Canceled) {
		return persistInterruptedPartial(*result, err)
	}
	kept := *result
	kept.Content = b.prose()
	fallback := kept.Content == "" && len(artifacts) > 0
	if fallback {
		kept.Content = fallbackArtifactResponse(artifacts[len(artifacts)-1])
	}
	if !persistInterruptedPartial(kept, err) {
		return false
	}
	if fallback {
		b.addText(kept.Content)
	}
	*result = kept
	return true
}

// IncognitoConfig is an incognito turn's fixed state: where its events go and
// how its model calls are attributed. It has no thread, message or plan.
type IncognitoConfig struct {
	Stream    Emitter
	Titles    *ReasoningTitleTracker
	Inference llm.InferenceMetadata
}

// RunIncognitoTurn runs a single, tool-free assistant turn for an ephemeral
// incognito thread. It mirrors RunAssistantLoop's len(tools)==0 fast path
// exactly: with no tools there are no persistence-capable side effects (no
// artifacts, no directive/memory writes), which is what lets an incognito turn
// answer while writing nothing. It hands out no Run, so none of the persisted
// turn's methods are reachable from it.
func (s *Engine) RunIncognitoTurn(ctx context.Context, c IncognitoConfig, history []llm.Message) (LoopResult, error) {
	t := &Run{e: s, stream: c.Stream, titles: c.Titles, inference: c.Inference}
	b := &blockBuilder{}
	result, err := t.streamAssistantTurn(ctx, b.nextReasoningID(), history, inferenceWithPurpose(t.inference, "chat", 1), nil)
	// Safety net: a tool-eager model may still emit an inline tool call
	// despite the no-tool prompt. The parser strips that markup — whether it
	// recovers a call or the markup is truncated/malformed and none is recovered —
	// leaving empty content. Since there are no tools to run, nudge it once to answer
	// directly rather than returning the empty (and therefore discarded) reply. Gated
	// on empty content alone, matching RunAssistantLoop's forced-final-answer.
	if err == nil && strings.TrimSpace(result.Content) == "" {
		slog.Info("incognito turn produced no answer text; retrying tool-free", "recovered_tool_calls", len(result.ToolCalls))
		retryHistory := append(append([]llm.Message(nil), history...), llm.Message{Role: "user", Content: incognitoDirectAnswerNudge})
		if retryResult, retryErr := t.streamAssistantTurn(ctx, b.nextReasoningID(), retryHistory, incognitoRetryInference(t.inference, result), nil); retryErr == nil && strings.TrimSpace(retryResult.Content) != "" {
			result = retryResult
		}
	}
	b.addResult(t.titles, result)
	if persistInterruptedPartial(result, err) {
		return b.result(result, nil, ""), nil
	}
	return b.result(result, nil, ""), err
}

// finalRetryInference picks the metadata for the retry of an empty forced final
// answer. One that ran out at the cap spent it reasoning, so the retry asks for
// the least reasoning; the same request again would run out the same way.
func finalRetryInference(metadata llm.InferenceMetadata, first llm.StreamResult) llm.InferenceMetadata {
	metadata = finalAnswerInference(metadata, "chat_final_retry", maxToolRounds+2)
	metadata.LeastReasoning = first.FinishReason == "length"
	return metadata
}

// incognitoRetryInference picks the metadata for the incognito empty-answer
// retry. A first turn that ran out at the cap spent it on reasoning, so the
// retry asks for the least reasoning on the forced final answer's budget.
func incognitoRetryInference(metadata llm.InferenceMetadata, first llm.StreamResult) llm.InferenceMetadata {
	if first.FinishReason == "length" {
		metadata = finalAnswerInference(metadata, "chat", 2)
		metadata.LeastReasoning = true
		return metadata
	}
	return inferenceWithPurpose(metadata, "chat", 2)
}

// streamAssistantTurn runs one model turn, relaying reasoning/content deltas and
// tool-call events to the SSE stream. t.titles/reasoningID let it spawn the
// reasoning abstract while the model is still reasoning (see
// defaultReasoningTitleStartBytes), or at the latest when it starts answering or
// calling a tool, so the title overlaps the turn instead of trailing it.
func (t *Run) streamAssistantTurn(ctx context.Context, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	return t.streamAssistantTurnWithContentStreaming(ctx, reasoningID, history, meta, tools, true)
}

func (t *Run) streamAssistantTurnSuppressingContent(ctx context.Context, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	return t.streamAssistantTurnWithContentStreaming(ctx, reasoningID, history, meta, tools, false)
}

func (t *Run) streamAssistantTurnWithContentStreaming(ctx context.Context, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool, streamContent bool) (llm.StreamResult, error) {
	callCtx := llm.WithInferenceMetadata(ctx, meta)
	var reasoningBuf strings.Builder
	titleSpawned := false
	var titleDone <-chan struct{}
	// Called once titleStartBytes of reasoning have streamed, and at
	// the reasoning->content (or reasoning->tool) boundary for a round that
	// never got that far. The first call wins; the title names the subject,
	// which the opening of the reasoning already carries.
	spawnTitle := func() {
		if titleSpawned {
			return
		}
		titleSpawned = true
		titleDone = t.titles.spawn(reasoningID, reasoningBuf.String())
	}
	// The title is what the reader looks at while the answer is on its way, so
	// it goes out before the first answer word, never after. A short thinker
	// finishes reasoning ~2.7s before its title call returns,
	// and the answer used to overtake it. Blocking here is safe: llmwire's
	// reader never blocks on its consumer, so the deltas queue and the idle
	// guard keeps measuring the model. titleHold bounds the wait.
	awaitTitle := func() error {
		if titleDone == nil {
			return nil
		}
		done := titleDone
		titleDone = nil
		hold := time.NewTimer(t.e.titleHold())
		defer hold.Stop()
		select {
		case <-done:
			return nil
		case <-hold.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return t.e.llm.StreamChatWithTools(callCtx, history, tools, func(event llm.StreamEvent) error {
		if event.ReasoningDelta != "" {
			if err := t.stream.Send("assistant_reasoning_delta", StreamDeltaResponse{Content: event.ReasoningDelta}); err != nil {
				return err
			}
			// The buffer only feeds the title, so it stops growing once that is spawned.
			if !titleSpawned {
				reasoningBuf.WriteString(event.ReasoningDelta)
				if reasoningBuf.Len() >= t.e.titleStartBytes() {
					spawnTitle()
				}
			}
			return nil
		}
		if event.ToolPending {
			spawnTitle()
			return t.stream.Send("tool_pending", struct{}{})
		}
		if event.Delta != "" && streamContent {
			spawnTitle()
			if err := awaitTitle(); err != nil {
				return err
			}
			return t.stream.Send("assistant_delta", StreamDeltaResponse{Content: event.Delta})
		}
		if event.ToolCall.ID != "" || event.ToolCall.Function.Name != "" {
			spawnTitle()
			return t.stream.Send("tool_call", ToolCallResponse{
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

// gateInference builds the metadata for a gate call (image intent,
// classification, drift, thread title) from the turn's identity alone, logged
// as round 1 since a gate is a single call. A gate is a short routing or
// titling call with its own budget and reasoning level, so nothing else set on
// the turn's metadata may reach it.
func gateInference(base llm.InferenceMetadata, purpose string) llm.InferenceMetadata {
	return llm.InferenceMetadata{
		UserID:   base.UserID,
		Username: base.Username,
		ThreadID: base.ThreadID,
		Purpose:  purpose,
		Round:    1,
	}
}

// finalAnswerMaxCompletionTokens is the completion budget for the forced final
// answer. It matches the default chat cap: the forced final synthesizes many
// gathered sources, and it must never be tighter than the answer a normal
// round could have written.
const finalAnswerMaxCompletionTokens = 16384

// finalAnswerInference builds the metadata for a forced final-answer turn: it
// widens the completion budget so a synthesis over many gathered sources has
// room to complete after the model's reasoning.
func finalAnswerInference(metadata llm.InferenceMetadata, purpose string, round int) llm.InferenceMetadata {
	metadata = inferenceWithPurpose(metadata, purpose, round)
	metadata.MaxCompletionTokens = finalAnswerMaxCompletionTokens
	return metadata
}
