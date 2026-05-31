package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) (string, error) {
	result, err := c.StreamChatWithTools(ctx, messages, nil, func(event StreamEvent) error {
		if event.Delta == "" || onDelta == nil {
			return nil
		}
		return onDelta(event.Delta)
	})
	return result.Content, err
}

func (c *Client) StreamChatWithTools(ctx context.Context, messages []Message, tools []Tool, onEvent func(StreamEvent) error) (StreamResult, error) {
	resp, err := c.executeChatRequestWithTools(ctx, messages, tools, true)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()

	var content strings.Builder
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
			return finishStream(content.String(), toolCalls, toolCallOrder, onEvent)
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return StreamResult{Content: content.String()}, fmt.Errorf("decode chat completion chunk: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onEvent != nil {
				if err := onEvent(StreamEvent{Delta: delta.Content}); err != nil {
					return StreamResult{Content: content.String()}, err
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
		return StreamResult{Content: content.String()}, fmt.Errorf("read chat completion stream: %w", err)
	}
	return finishStream(content.String(), toolCalls, toolCallOrder, onEvent)
}

func finishStream(content string, byIndex map[int]*ToolCall, order []int, onEvent func(StreamEvent) error) (StreamResult, error) {
	result := StreamResult{Content: content, ToolCalls: make([]ToolCall, 0, len(order))}
	for _, index := range order {
		call := *byIndex[index]
		result.ToolCalls = append(result.ToolCalls, call)
		if onEvent != nil {
			if err := onEvent(StreamEvent{ToolCall: call}); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
