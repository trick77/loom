package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/sse"
)

const sparkSystemPrompt = "You are Spark, a concise assistant for work and school. Answer in the user's language unless their profile requests a specific response language."

func (s *server) handleStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	var body streamMessageRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	threadID := r.PathValue("threadID")
	thread, found, err := s.chat.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	priorMessages, found, err := s.chat.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list messages failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	userMessage, err := s.chat.AddMessage(r.Context(), user.ID, threadID, chat.RoleUser, body.Content)
	if err != nil {
		writeChatStoreError(w, err, http.StatusBadRequest, "message content is required", "message content is too long")
		return
	}

	stream, err := sse.NewWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := sendSSEJSON(stream, "user_message", userMessage); err != nil {
		return
	}

	history := buildLLMHistory(user, priorMessages, userMessage)
	assistantContent, err := s.runAssistantLoop(r.Context(), stream, history)
	if err != nil {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "stream failed"})
		return
	}
	if strings.TrimSpace(assistantContent) == "" {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "empty assistant response"})
		return
	}

	persistCtx := context.WithoutCancel(r.Context())
	assistantMessage, err := s.chat.AddMessage(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent)
	if err != nil {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "persist assistant message failed"})
		return
	}
	if err := sendSSEJSON(stream, "assistant_message", assistantMessage); err != nil {
		return
	}

	if thread.Title == chat.DefaultThreadTitle {
		_ = s.generateAndSendThreadTitle(r.Context(), persistCtx, stream, user.ID, threadID, userMessage.Content, assistantMessage.Content)
	}
	_ = stream.Send("done", "{}")
}

const maxToolRounds = 4

func (s *server) runAssistantLoop(ctx context.Context, stream *sse.Writer, history []llm.Message) (string, error) {
	tools := []llm.Tool(nil)
	if s.mcp != nil {
		tools = s.mcp.Tools()
	}
	if len(tools) == 0 {
		return s.llm.StreamChat(ctx, history, func(delta string) error {
			return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: delta})
		})
	}

	for range maxToolRounds {
		result, err := s.llm.StreamChatWithTools(ctx, history, tools, func(event llm.StreamEvent) error {
			if event.Delta != "" {
				return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
			}
			if event.ToolCall.ID != "" {
				return sendSSEJSON(stream, "tool_call", toolCallResponse{
					ID:        event.ToolCall.ID,
					Name:      event.ToolCall.Function.Name,
					Arguments: event.ToolCall.Function.Arguments,
				})
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		if len(result.ToolCalls) == 0 {
			return result.Content, nil
		}

		history = append(history, llm.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})
		for _, call := range result.ToolCalls {
			arguments, err := parseToolArguments(call.Function.Arguments)
			if err != nil {
				return "", err
			}
			output, err := s.mcp.CallTool(ctx, call.Function.Name, arguments)
			if err != nil {
				return "", err
			}
			if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				return "", err
			}
			history = append(history, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    output,
			})
		}
	}
	return "", nil
}

func parseToolArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}

func (s *server) generateAndSendThreadTitle(requestCtx, persistCtx context.Context, stream *sse.Writer, userID, threadID, userMessage, assistantMessage string) error {
	title, err := s.llm.GenerateTitle(requestCtx, userMessage, assistantMessage)
	if err != nil {
		return err
	}
	thread, found, err := s.chat.UpdateThread(persistCtx, userID, threadID, chat.UpdateThreadInput{Title: &title})
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return sendSSEJSON(stream, "thread", thread)
}

func buildLLMHistory(user auth.User, messages []chat.Message, newUserMessage chat.Message) []llm.Message {
	history := []llm.Message{{Role: "system", Content: systemPromptForUser(user)}}
	for _, message := range messages {
		switch message.Role {
		case chat.RoleUser, chat.RoleAssistant:
			history = append(history, llm.Message{Role: string(message.Role), Content: message.Content})
		}
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage.Content})
	return history
}

func systemPromptForUser(user auth.User) string {
	if user.ResponseLanguage == "" || strings.EqualFold(user.ResponseLanguage, "auto") {
		return sparkSystemPrompt
	}
	return sparkSystemPrompt + "\nAlways answer in this language: " + user.ResponseLanguage + "."
}

func sendSSEJSON(stream *sse.Writer, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return stream.Send(event, string(payload))
}
