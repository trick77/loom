package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) (string, error) {
	result, err := c.StreamChatResult(ctx, messages, onDelta)
	return result.Content, err
}

func (c *Client) StreamChatResult(ctx context.Context, messages []Message, onDelta func(string) error) (StreamResult, error) {
	result, err := c.StreamChatWithTools(ctx, messages, nil, func(event StreamEvent) error {
		if event.Delta == "" || onDelta == nil {
			return nil
		}
		return onDelta(event.Delta)
	})
	return result, err
}

func (c *Client) StreamChatWithTools(ctx context.Context, messages []Message, tools []Tool, onEvent func(StreamEvent) error) (StreamResult, error) {
	start := time.Now()
	resp, err := c.executeChatRequestWithTools(ctx, messages, tools, true)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return StreamResult{}, err
	}
	defer resp.Body.Close()

	var content strings.Builder
	var reasoning strings.Builder
	var usage TokenUsage
	toolCalls := map[int]*ToolCall{}
	var toolCallOrder []int
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			result, err := finishStream(content.String(), reasoning.String(), usage, toolCalls, toolCallOrder, onEvent, isMiMoModel(c.model))
			if err != nil {
				logInferenceFailed(ctx, c.model, time.Since(start), err)
				return result, err
			}
			result.Duration = time.Since(start)
			result.Model = c.model
			logInferenceCompleted(ctx, c.model, result.Duration, result.Usage)
			return result, nil
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			err := fmt.Errorf("decode chat completion chunk: %w", err)
			logInferenceFailed(ctx, c.model, time.Since(start), err)
			return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
		}
		if chunk.Usage.Present() {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		if delta.ReasoningContent != "" {
			reasoning.WriteString(delta.ReasoningContent)
			if onEvent != nil {
				if err := onEvent(StreamEvent{ReasoningDelta: delta.ReasoningContent}); err != nil {
					logInferenceFailed(ctx, c.model, time.Since(start), err)
					return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
				}
			}
		}
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onEvent != nil {
				if err := onEvent(StreamEvent{Delta: delta.Content}); err != nil {
					logInferenceFailed(ctx, c.model, time.Since(start), err)
					return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
				}
			}
		}
		for _, chunk := range delta.ToolCalls {
			call, ok := toolCalls[chunk.Index]
			if !ok {
				call = &ToolCall{Type: "function"}
				toolCalls[chunk.Index] = call
				toolCallOrder = append(toolCallOrder, chunk.Index)
			}
			if chunk.ID != "" {
				call.ID = chunk.ID
			}
			if chunk.Type != "" {
				call.Type = chunk.Type
			}
			if chunk.Function.Name != "" {
				call.Function.Name += chunk.Function.Name
			}
			if chunk.Function.Arguments != "" {
				call.Function.Arguments += chunk.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		err := fmt.Errorf("read chat completion stream: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
	}
	result, err := finishStream(content.String(), reasoning.String(), usage, toolCalls, toolCallOrder, onEvent, isMiMoModel(c.model))
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return result, err
	}
	result.Duration = time.Since(start)
	result.Model = c.model
	logInferenceCompleted(ctx, c.model, result.Duration, result.Usage)
	return result, nil
}

func finishStream(content string, reasoningContent string, usage TokenUsage, byIndex map[int]*ToolCall, order []int, onEvent func(StreamEvent) error, parseInlineTools bool) (StreamResult, error) {
	result := StreamResult{
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        make([]ToolCall, 0, len(order)),
		Usage:            usage,
	}
	for _, index := range order {
		call := *byIndex[index]
		result.ToolCalls = append(result.ToolCalls, call)
		if onEvent != nil {
			if err := onEvent(StreamEvent{ToolCall: call}); err != nil {
				return result, err
			}
		}
	}
	// Models that lack native tool_calls (e.g. MiMo) emit the call as inline XML
	// in the content. Recover those only when the API returned no native calls.
	if parseInlineTools && len(result.ToolCalls) == 0 {
		inlineCalls, cleaned := parseInlineToolCalls(result.Content)
		for _, call := range inlineCalls {
			result.ToolCalls = append(result.ToolCalls, call)
			if onEvent != nil {
				if err := onEvent(StreamEvent{ToolCall: call}); err != nil {
					return result, err
				}
			}
		}
		if len(inlineCalls) > 0 {
			result.Content = cleaned
		}
	}
	return result, nil
}
