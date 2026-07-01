package httpapi

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// messageMetricsFromTurn builds the persisted per-message stats. Model, reasoning
// effort, and reasoning content describe the final answer call (result), while
// usage and duration cover the whole turn: usage is the sum across every model
// call (answer turns, tool rounds, and the reasoning/thread-title helpers) and
// duration is the turn's wall-clock. ContextTokens is the exception — it is the
// final answer call's own model-reported total_tokens (result.Usage), the true
// context size of that single generation, kept separate from the accumulated
// usage so the UI can report context-window occupancy without double-counting.
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
	// ContextTokens is the final answer call's own model-reported total_tokens (the
	// real size of that single generation's context), not the per-turn accumulated
	// `usage` total which double-counts the prompt across tool rounds and helper
	// calls. This is the correct basis for the context-window percentage shown in
	// the UI. Sourced from the returned StreamResult, which is always the final
	// answer call.
	if result.Usage.TotalTokens > 0 {
		metrics.ContextTokens = intPtr(result.Usage.TotalTokens)
	}
	return metrics
}

func intPtr(value int) *int {
	return &value
}

func strPtr(value string) *string {
	return &value
}

// generateAndSendThreadTitle titles and classifies the first message, persists
// both onto the thread, and emits the updated thread over SSE. The title and
// category come from two separate utility calls run concurrently (added latency ≈
// one call); classification is best-effort and falls back to General. It returns
// the chosen category so the caller can inject the matching system-prompt block on
// this very turn (both calls finish before the answer history is built).
//
// When categoryOverride is non-empty the classify call is skipped entirely and
// the override is used as the category. The caller passes this for requests it
// has already routed deterministically (e.g. image generation), where the
// text-classifier's guess would be both wrong and pointless.
func (s *server) generateAndSendThreadTitle(requestCtx, persistCtx context.Context, stream *sse.Writer, user auth.User, threadID, userMessage, assistantMessage, categoryOverride string) (string, error) {
	titleInference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID, Purpose: "title", Round: 1}
	classifyInference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID, Purpose: "classify", Round: 1}

	var (
		title    string
		titleErr error
		category = string(classifier.General)
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		title, titleErr = s.llm.GenerateThreadTitle(llm.WithInferenceMetadata(requestCtx, titleInference), userMessage, assistantMessage, userResponseLanguage(user))
	}()
	if categoryOverride != "" {
		category = categoryOverride
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ClassifyThread always returns a valid category (General on failure); the
			// error is informational, so a failed classification never blocks the title.
			category, _ = s.llm.ClassifyThread(llm.WithInferenceMetadata(requestCtx, classifyInference), userMessage)
		}()
	}
	wg.Wait()

	if titleErr != nil {
		return category, titleErr
	}
	thread, found, err := s.thread.UpdateThread(persistCtx, user.ID, threadID, chat.UpdateThreadInput{Title: &title, Category: &category})
	if err != nil {
		return category, err
	}
	if !found {
		return category, nil
	}
	// A newly-titled thread in a project changes the project's titled-thread set, so
	// refresh its big-picture description (debounced/count-gated, so this is cheap and
	// fires real work only when the set actually changed). Best-effort, off the hot path.
	if thread.ProjectID != nil {
		s.maybeRefreshProjectDescriptionAsync(persistCtx, user, *thread.ProjectID)
	}
	return category, sendSSEJSON(stream, "thread", thread)
}

func buildLLMHistory(user auth.User, classifierContext, userContext, projectContext, knowledgeContext, documentContext string, messages []chat.Message, newUserMessage chat.Message) []llm.Message {
	systemContent := systemPromptForUser(user, time.Now())
	if strings.TrimSpace(classifierContext) != "" {
		systemContent += "\n\n" + classifierContext
	}
	if strings.TrimSpace(userContext) != "" {
		systemContent += "\n\n" + userContext
	}
	if strings.TrimSpace(projectContext) != "" {
		systemContent += "\n\n" + projectContext
	}
	if strings.TrimSpace(knowledgeContext) != "" {
		systemContent += "\n\n" + knowledgeContext
	}
	if strings.TrimSpace(documentContext) != "" {
		systemContent += "\n\n" + documentContext
	}
	history := []llm.Message{{Role: "system", Content: systemContent}}
	for _, message := range messages {
		switch message.Role {
		case chat.RoleUser, chat.RoleAssistant:
			history = append(history, llm.Message{
				Role:    string(message.Role),
				Content: message.Content,
			})
		}
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage.Content})
	return history
}

// incognitoSystemPrompt is the system prompt for a tool-free incognito turn. Unlike
// loomSystemPrompt it grants NO tools and explicitly forbids tool/search/file calls,
// so a tool-eager model (notably MiMo, whose inline tool-call markup is stripped from
// the content) answers directly instead of emitting a call that would be recovered
// and stripped — leaving an empty reply on the no-tool incognito path.
const incognitoSystemPrompt = "You are Loom in an incognito conversation. Default to flowing prose — full sentences grouped into paragraphs — when explaining or describing something. Reach for markdown structure only when it genuinely helps the reader: a list for a true enumeration, a table to compare items across the same dimensions, and headings only for long, multi-section answers. Keep short or simple answers as plain prose. Use **bold** sparingly for key terms, and put code in fenced markdown blocks. You have NO tools available in this conversation: do not search the web, browse or fetch URLs, look up past conversations, generate images, or create files, and never emit a tool call of any kind. Answer directly from your own knowledge. If you do not know something, or it may be out of date or beyond your knowledge, say so plainly instead of claiming to look it up or guessing."

// incognitoDirectAnswerNudge is appended as a final user turn when the first
// incognito attempt comes back empty because the model tried to call a tool anyway
// (its inline markup was stripped). It forces a direct, tool-free answer.
const incognitoDirectAnswerNudge = "Answer my previous message directly now, in prose, using only your own knowledge. Do not call, attempt, or describe any tool, search, browse, or file operation."

func incognitoSystemPromptForUser(user auth.User, now time.Time) string {
	dateLine := "\nThe current date is " + now.Format("2006-01-02") + ". Treat this as today when interpreting time-relative requests; do not assume an earlier year."
	if user.ResponseLanguage == "" || strings.EqualFold(user.ResponseLanguage, "auto") {
		return incognitoSystemPrompt + "\nAlways answer in English." + dateLine
	}
	return incognitoSystemPrompt + "\nAlways answer in this language: " + languageName(user.ResponseLanguage) + "." + dateLine
}

// buildIncognitoHistory assembles the model history for an incognito turn: the
// tool-free incognito system prompt, the client-supplied prior turns, then the new
// user message. It reads no persisted memory or context (mirroring the "not added to
// memory" promise on the read side too).
func buildIncognitoHistory(user auth.User, messages []chat.Message, newUserMessage chat.Message) []llm.Message {
	history := []llm.Message{{Role: "system", Content: incognitoSystemPromptForUser(user, time.Now())}}
	for _, message := range messages {
		switch message.Role {
		case chat.RoleUser, chat.RoleAssistant:
			history = append(history, llm.Message{Role: string(message.Role), Content: message.Content})
		}
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage.Content})
	return history
}

func shouldGenerateThreadTitle(currentTitle, firstPrompt string) bool {
	if currentTitle == chat.DefaultThreadTitle {
		return true
	}
	return currentTitle == chat.NormalizeThreadTitle(firstPrompt)
}

func systemPromptForUser(user auth.User, now time.Time) string {
	dateLine := "\nThe current date is " + now.Format("2006-01-02") + ". Treat this as today when interpreting time-relative requests and when constructing search queries; do not assume an earlier year."
	if user.ResponseLanguage == "" || strings.EqualFold(user.ResponseLanguage, "auto") {
		return loomSystemPrompt + "\nAlways answer in English." + dateLine
	}
	return loomSystemPrompt + "\nAlways answer in this language: " + languageName(user.ResponseLanguage) + "." + dateLine
}

// userResponseLanguage resolves the language a user-facing utility generation
// (thread title, project description, reasoning title, project memory) should be
// written in, mirroring systemPromptForUser so these match the language the chat
// answers in. Empty (auto/unset) means the English default, which needs no
// directive.
func userResponseLanguage(user auth.User) string {
	if user.ResponseLanguage == "" || strings.EqualFold(user.ResponseLanguage, "auto") {
		return ""
	}
	return languageName(user.ResponseLanguage)
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
