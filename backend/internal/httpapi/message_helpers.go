package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/sse"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// messageMetricsFromTurn builds the persisted per-message stats. Model, reasoning
// effort, and reasoning content describe the final answer call (result), while
// usage and duration cover the whole turn: usage is the sum across every model
// call (answer turns, tool rounds, and the reasoning/thread-title helpers) and
// duration is the turn's wall-clock.
func messageMetricsFromTurn(result llm.StreamResult, usage llm.TokenUsage, duration time.Duration) chat.MessageTokenUsage {
	metrics := chat.MessageTokenUsage{ReasoningContent: result.ReasoningContent}
	if result.Model != "" {
		metrics.Model = strPtr(result.Model)
	}
	if result.ReasoningEffort != "" {
		metrics.ReasoningEffort = strPtr(result.ReasoningEffort)
	}
	if duration > 0 {
		metrics.DurationMs = intPtr(int(duration.Milliseconds()))
	}
	if usage.Present() {
		metrics.PromptTokens = intPtr(usage.PromptTokens)
		metrics.CompletionTokens = intPtr(usage.CompletionTokens)
		metrics.TotalTokens = intPtr(usage.TotalTokens)
		metrics.CachedTokens = intPtr(usage.PromptTokensDetails.CachedTokens)
		metrics.ReasoningTokens = intPtr(usage.CompletionTokenDetails.ReasoningTokens)
	}
	return metrics
}

func intPtr(value int) *int {
	return &value
}

func strPtr(value string) *string {
	return &value
}

func (s *server) generateAndSendThreadTitle(requestCtx, persistCtx context.Context, stream *sse.Writer, user auth.User, threadID, userMessage, assistantMessage string) error {
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID, Purpose: "title", Round: 1}
	title, err := s.llm.GenerateChatTitle(llm.WithInferenceMetadata(requestCtx, inference), userMessage, assistantMessage)
	if err != nil {
		return err
	}
	thread, found, err := s.chat.UpdateThread(persistCtx, user.ID, threadID, chat.UpdateThreadInput{Title: &title})
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return sendSSEJSON(stream, "thread", thread)
}

// startMCPStatus begins a live reachability probe of the configured MCP servers
// in the background so its latency overlaps with assistant generation. It
// returns nil when MCP is disabled. The channel is buffered, so the probe never
// leaks even if the caller returns early without reading it.
func (s *server) startMCPStatus(ctx context.Context) <-chan mcpStatusResponse {
	if s.mcp == nil {
		return nil
	}
	ch := make(chan mcpStatusResponse, 1)
	go func() {
		ch <- s.currentMCPStatus(ctx)
	}()
	return ch
}

// sendMCPStatus emits a best-effort mcp_status event from a probe started by
// startMCPStatus. It is skipped when MCP is disabled or no servers are
// configured, and never aborts the stream.
func sendMCPStatus(stream *sse.Writer, ch <-chan mcpStatusResponse) {
	if ch == nil {
		return
	}
	status := <-ch
	if status.Configured == 0 {
		return
	}
	_ = sendSSEJSON(stream, "mcp_status", status)
}

func buildLLMHistory(user auth.User, messages []chat.Message, newUserMessage chat.Message) []llm.Message {
	history := []llm.Message{{Role: "system", Content: systemPromptForUser(user)}}
	for _, message := range messages {
		switch message.Role {
		case chat.RoleUser, chat.RoleAssistant:
			history = append(history, llm.Message{
				Role:             string(message.Role),
				Content:          message.Content,
				ReasoningContent: message.ReasoningContent,
			})
		}
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage.Content})
	return history
}

func systemPromptForUser(user auth.User) string {
	if user.ResponseLanguage == "" || strings.EqualFold(user.ResponseLanguage, "auto") {
		return sloprSystemPrompt + "\nAlways answer in English."
	}
	return sloprSystemPrompt + "\nAlways answer in this language: " + languageName(user.ResponseLanguage) + "."
}

// languageName resolves a profile language value to its English name (for
// example "de" -> "German"). Values that are not valid language tags — such as
// a name that is already spelled out — are returned unchanged.
func languageName(value string) string {
	tag, err := language.Parse(value)
	if err != nil {
		return value
	}
	if name := display.English.Tags().Name(tag); name != "" {
		return name
	}
	return value
}
