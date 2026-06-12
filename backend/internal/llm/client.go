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
const defaultMaxCompletionTokens = 2048

// documentToolMaxCompletionTokens gives document-generation tool rounds enough
// room to serialize structured file payloads. The budget is only a ceiling: if
// a round actually calls a small tool such as web search, the model is not
// forced to consume it.
const documentToolMaxCompletionTokens = 8192

// documentToolTimeout gives model turns that are serializing complete document
// payloads enough wall-clock time to reach the tool call.
const documentToolTimeout = 5 * time.Minute

// Config holds the OpenAI-compatible chat completion settings.
type Config struct {
	BaseURL             string
	APIKey              string
	Model               string
	ReasoningEffort     string
	MaxCompletionTokens int
	Timeout             time.Duration
	ResponseLogDir      string
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
	baseURL             string
	apiKey              string
	model               string
	reasoningEffort     string
	maxCompletionTokens int
	timeout             time.Duration
	httpClient          *http.Client
	responseLogDir      string
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
	maxCompletionTokens := cfg.MaxCompletionTokens
	if maxCompletionTokens <= 0 {
		maxCompletionTokens = defaultMaxCompletionTokens
	}
	return &Client{
		baseURL:             strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:              cfg.APIKey,
		model:               cfg.Model,
		reasoningEffort:     reasoningEffort,
		maxCompletionTokens: maxCompletionTokens,
		timeout:             cfg.Timeout,
		httpClient:          httpClient,
		responseLogDir:      cfg.ResponseLogDir,
	}
}

// isMiMoModel reports whether the configured model is a MiMo variant. It uses a
// substring match so deploy names like "MiMo-7B" or "mimo-vl" are recognized,
// not just the bare "mimo".
func isMiMoModel(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "mimo")
}

// utilityMaxCompletionTokens hard-caps secondary helper calls (titles). A clean
// title is a handful of tokens; the cap is a guard so a misbehaving turn can
// never run long. Sized with headroom for an 8-word gerund title — a call that
// still hits the cap is treated as truncated and discarded (see title decoders).
const utilityMaxCompletionTokens = 32

type chatRequestOptions struct {
	tools               []Tool
	stream              bool
	reasoningEffort     string
	thinking            *thinkingOption
	maxCompletionTokens int
}

func (c *Client) executeChatRequest(ctx context.Context, messages []Message, stream bool) (*http.Response, error) {
	return c.executeChatRequestImpl(ctx, messages, chatRequestOptions{
		stream:              stream,
		reasoningEffort:     c.reasoningEffort,
		maxCompletionTokens: c.maxCompletionTokens,
	})
}

func (c *Client) executeChatRequestWithTools(ctx context.Context, messages []Message, tools []Tool, stream bool) (*http.Response, error) {
	return c.executeChatRequestImpl(ctx, messages, chatRequestOptions{
		tools:               tools,
		stream:              stream,
		reasoningEffort:     c.reasoningEffort,
		maxCompletionTokens: c.maxCompletionTokensForTools(tools),
	})
}

// executeUtilityChatRequest runs a non-streaming secondary helper call (title or
// reasoning-abstract generation) with thinking turned off via MiMo's native
// {"thinking":{"type":"disabled"}}. Default thinking makes MiMo overthink a
// trivial summarization and even echo its internal "reasoning>/response>"
// channel format as literal text instead of a clean title — besides burning ~1k
// reasoning tokens per call.
func (c *Client) executeUtilityChatRequest(ctx context.Context, messages []Message) (*http.Response, error) {
	return c.executeChatRequestImpl(ctx, messages, chatRequestOptions{
		thinking:            &thinkingOption{Type: "disabled"},
		maxCompletionTokens: utilityMaxCompletionTokens,
	})
}

func (c *Client) executeChatRequestImpl(ctx context.Context, messages []Message, opts chatRequestOptions) (*http.Response, error) {
	requestBody := chatCompletionRequest{
		Model:               c.model,
		Messages:            messages,
		Stream:              opts.stream,
		Tools:               opts.tools,
		ReasoningEffort:     opts.reasoningEffort,
		Thinking:            opts.thinking,
		MaxCompletionTokens: opts.maxCompletionTokens,
	}
	if opts.stream {
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

func (c *Client) maxCompletionTokensForTools(tools []Tool) int {
	if !hasDocumentGenerationTool(tools) || c.maxCompletionTokens >= documentToolMaxCompletionTokens {
		return c.maxCompletionTokens
	}
	return documentToolMaxCompletionTokens
}

func (c *Client) timeoutForTools(tools []Tool) time.Duration {
	if !hasDocumentGenerationTool(tools) || c.timeout >= documentToolTimeout {
		return c.timeout
	}
	return documentToolTimeout
}

func hasDocumentGenerationTool(tools []Tool) bool {
	for _, tool := range tools {
		// Keep this list in sync with backend/internal/docgen generator
		// ToolName methods. The llm package intentionally stays a leaf package
		// and does not import docgen.
		switch tool.Function.Name {
		case "create_text_file",
			"create_pdf_file",
			"create_xlsx_file",
			"create_docx_file",
			"create_pptx_presentation":
			return true
		}
	}
	return false
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
