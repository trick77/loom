package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxErrorBodyBytes = 4096

// Config holds the OpenAI-compatible chat completion settings.
type Config struct {
	BaseURL         string
	APIKey          string
	Model           string
	ReasoningEffort string
	ResponseLogDir  string
}

// Message is one OpenAI-compatible chat message.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

// Client calls an OpenAI-compatible chat completion API.
type Client struct {
	baseURL         string
	apiKey          string
	model           string
	reasoningEffort string
	httpClient      *http.Client
	responseLogDir  string
}

var responseLogSequence uint64

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
		responseLogDir:  cfg.ResponseLogDir,
	}
}

func isMiMoModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "mimo")
}

func (c *Client) executeChatRequest(ctx context.Context, messages []Message, stream bool) (*http.Response, error) {
	return c.executeChatRequestWithTools(ctx, messages, nil, stream)
}

func (c *Client) executeChatRequestWithTools(ctx context.Context, messages []Message, tools []Tool, stream bool) (*http.Response, error) {
	requestBody := chatCompletionRequest{
		Model:           c.model,
		Messages:        messages,
		Stream:          stream,
		Tools:           tools,
		ReasoningEffort: c.reasoningEffort,
	}
	if stream {
		requestBody.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(requestBody)
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
	c.wrapResponseLogger(resp)
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

func (c *Client) wrapResponseLogger(resp *http.Response) {
	if c.responseLogDir == "" || resp == nil || resp.Body == nil {
		return
	}
	resp.Body = &responseLoggingBody{
		ReadCloser: resp.Body,
		resp:       resp,
		logDir:     c.responseLogDir,
	}
}

type responseLoggingBody struct {
	io.ReadCloser
	resp   *http.Response
	logDir string
	body   bytes.Buffer
	once   sync.Once
}

func (b *responseLoggingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.body.Write(p[:n])
	}
	return n, err
}

func (b *responseLoggingBody) Close() error {
	closeErr := b.ReadCloser.Close()
	b.once.Do(func() {
		if err := b.writeLog(); err != nil {
			// Response logs are a local-dev diagnostic aid; never fail chat delivery because logging failed.
		}
	})
	return closeErr
}

func (b *responseLoggingBody) writeLog() error {
	if err := os.MkdirAll(b.logDir, 0o700); err != nil {
		return err
	}
	var out bytes.Buffer
	proto := b.resp.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	_, _ = fmt.Fprintf(&out, "%s %s\n", proto, b.resp.Status)
	if err := b.resp.Header.Write(&out); err != nil {
		return err
	}
	_, _ = out.WriteString("\n")
	_, _ = b.body.WriteTo(&out)

	seq := atomic.AddUint64(&responseLogSequence, 1)
	name := fmt.Sprintf("%s-%06d.http", time.Now().UTC().Format("20060102T150405.000000000Z"), seq)
	return os.WriteFile(filepath.Join(b.logDir, name), out.Bytes(), 0o600)
}
