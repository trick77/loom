package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) (string, error) {
	resp, err := c.executeChatRequest(ctx, messages, true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var content strings.Builder
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
			return content.String(), nil
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return content.String(), fmt.Errorf("decode chat completion chunk: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}

		content.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return content.String(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return content.String(), fmt.Errorf("read chat completion stream: %w", err)
	}
	return content.String(), nil
}
