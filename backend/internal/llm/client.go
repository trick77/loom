package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxErrorBodyBytes = 4096

// Config holds the OpenAI-compatible chat completion settings.
type Config struct {
	BaseURL         string
	APIKey          string
	Model           string
	ReasoningEffort string
}

// Message is one OpenAI-compatible chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Client calls an OpenAI-compatible chat completion API.
type Client struct {
	baseURL         string
	apiKey          string
	model           string
	reasoningEffort string
	httpClient      *http.Client
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	reasoningEffort := cfg.ReasoningEffort
	if reasoningEffort == "" && isMiMoModel(cfg.Model) {
		reasoningEffort = "high"
	}
	return &Client{
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:          cfg.APIKey,
		model:           cfg.Model,
		reasoningEffort: reasoningEffort,
		httpClient:      httpClient,
	}
}

func isMiMoModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "mimo")
}

func (c *Client) executeChatRequest(ctx context.Context, messages []Message, stream bool) (*http.Response, error) {
	return c.executeChatRequestWithTools(ctx, messages, nil, stream)
}

func (c *Client) executeChatRequestWithTools(ctx context.Context, messages []Message, tools []Tool, stream bool) (*http.Response, error) {
	body, err := json.Marshal(chatCompletionRequest{
		Model:           c.model,
		Messages:        messages,
		Stream:          stream,
		Tools:           tools,
		ReasoningEffort: c.reasoningEffort,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal chat completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chat completion request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat completion request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if readErr != nil {
			return nil, fmt.Errorf("chat completion failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("chat completion failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}
