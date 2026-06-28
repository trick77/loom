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
//
// Sized at 32k after a real document turn hit the old 8192 cap mid-serialization:
// the model emits the whole file as a single tool-call argument, so once it runs
// past the cap finish_reason=length truncates the argument JSON, which both fails
// the document tool and (when the broken call is replayed) makes the upstream
// reject the next round. 32k clears any realistic document while leaving
// context-window headroom for the prompt. (MiMo 2.5 Pro's real input window is
// materially smaller than the 131072 once noted here — do not size budgets
// against that figure; it was never verified against the deployment.)
const documentToolMaxCompletionTokens = 32768

// documentToolTimeout gives model turns that are serializing complete document
// payloads enough wall-clock time to reach the tool call.
const documentToolTimeout = 5 * time.Minute

// Hardcoded MiMo model selection. Loom targets MiMo specifically and is no
// longer model-configurable: textModel handles normal (text-only) turns, and
// visionModel (the omnimodal non-Pro variant) is used only for turns that carry
// image input — mimo-v2.5-pro is text-only and 404s on any image_url part.
const (
	textModel              = "mimo-v2.5-pro"
	visionModel            = "mimo-v2.5"
	defaultReasoningEffort = "high"
)

// Config holds the OpenAI-compatible chat completion settings.
type Config struct {
	BaseURL             string
	APIKey              string
	MaxCompletionTokens int
	Timeout             time.Duration
	// IdleTimeout aborts a stream when no chunk arrives within the window. Zero
	// disables the watchdog (the coarse total Timeout still applies).
	IdleTimeout    time.Duration
	ResponseLogDir string
}

// Message is one OpenAI-compatible chat message.
type Message struct {
	Role             string               `json:"role"`
	Content          string               `json:"content,omitempty"`
	ContentParts     []MessageContentPart `json:"-"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall           `json:"tool_calls,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
}

type MessageContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *MessageImageURL `json:"image_url,omitempty"`
}

type MessageImageURL struct {
	URL string `json:"url"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	type messageAlias Message
	if len(m.ContentParts) == 0 {
		return json.Marshal(messageAlias(m))
	}
	return json.Marshal(struct {
		Role             string               `json:"role"`
		Content          []MessageContentPart `json:"content"`
		ReasoningContent string               `json:"reasoning_content,omitempty"`
		ToolCalls        []ToolCall           `json:"tool_calls,omitempty"`
		ToolCallID       string               `json:"tool_call_id,omitempty"`
	}{
		Role:             m.Role,
		Content:          m.ContentParts,
		ReasoningContent: m.ReasoningContent,
		ToolCalls:        m.ToolCalls,
		ToolCallID:       m.ToolCallID,
	})
}

// Client calls an OpenAI-compatible chat completion API.
type Client struct {
	baseURL             string
	apiKey              string
	model               string
	visionModel         string
	reasoningEffort     string
	maxCompletionTokens int
	timeout             time.Duration
	idleTimeout         time.Duration
	httpClient          *http.Client
	responseLogDir      string
}

var responseLogSequence uint64

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	maxCompletionTokens := cfg.MaxCompletionTokens
	if maxCompletionTokens <= 0 {
		maxCompletionTokens = defaultMaxCompletionTokens
	}
	return &Client{
		baseURL:             strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:              cfg.APIKey,
		model:               textModel,
		visionModel:         visionModel,
		reasoningEffort:     defaultReasoningEffort,
		maxCompletionTokens: maxCompletionTokens,
		timeout:             cfg.Timeout,
		idleTimeout:         cfg.IdleTimeout,
		httpClient:          httpClient,
		responseLogDir:      cfg.ResponseLogDir,
	}
}

// ModelSummary describes the hardcoded chat models for the startup capability line.
func ModelSummary() string {
	return textModel + " (text) / " + visionModel + " (vision)"
}

// isMiMoModel reports whether the model is a MiMo variant. Both hardcoded models
// (text and vision) are MiMo, so this is effectively always true; it is kept to
// gate MiMo-specific stream handling (inline tool-call parsing) at the call site.
func isMiMoModel(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "mimo")
}

// modelForMessages selects the chat model for a request: the omnimodal vision
// model when any message carries an image_url content part, otherwise the
// text-only model. This is the single routing decision; callers thread the
// returned name through the request body and into StreamResult.Model so the
// persisted/observed model reflects what actually ran.
func (c *Client) modelForMessages(messages []Message) string {
	for _, m := range messages {
		for _, part := range m.ContentParts {
			if part.Type == "image_url" {
				return c.visionModel
			}
		}
	}
	return c.model
}

// utilityMaxCompletionTokens hard-caps secondary helper calls (titles). A clean
// title is a handful of tokens; the cap is a guard so a misbehaving turn can
// never run long. Sized with headroom for an 8-word gerund title — a call that
// still hits the cap is treated as truncated and discarded (see title decoders).
const utilityMaxCompletionTokens = 32

type chatRequestOptions struct {
	model               string
	tools               []Tool
	stream              bool
	reasoningEffort     string
	thinking            *thinkingOption
	maxCompletionTokens int
}

func (c *Client) executeChatRequest(ctx context.Context, messages []Message, stream bool) (*http.Response, error) {
	return c.executeChatRequestImpl(ctx, messages, chatRequestOptions{
		model:               c.modelForMessages(messages),
		stream:              stream,
		reasoningEffort:     c.reasoningEffort,
		maxCompletionTokens: c.maxCompletionTokens,
	})
}

func (c *Client) executeChatRequestWithTools(ctx context.Context, messages []Message, tools []Tool, stream bool, model string) (*http.Response, error) {
	return c.executeChatRequestImpl(ctx, messages, chatRequestOptions{
		model:               model,
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
	model := opts.model
	if model == "" {
		model = c.model
	}
	requestBody := chatCompletionRequest{
		Model:               model,
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

// timeoutForTools is the coarse total wall-clock budget for a streamed turn. It
// stays generous (documentToolTimeout) whenever a document tool is on offer,
// because a turn that streams a full document payload as a tool-call argument
// legitimately needs the room — and intent cannot be known up front (MiMo only
// surfaces the tool name once the stream ends). The idle watchdog catches a
// stalled reasoning/content phase in seconds; but once a document tool call is
// underway it widens (see toolCallIdleTimeout) because MiMo buffers the argument
// server-side, so this coarse deadline is the real backstop for that phase.
func (c *Client) timeoutForTools(tools []Tool) time.Duration {
	if c.timeout == 0 || !hasDocumentGenerationTool(tools) || c.timeout >= documentToolTimeout {
		return c.timeout
	}
	return documentToolTimeout
}

// toolCallIdleTimeout is the idle window to apply once a tool call is underway in
// a turn that can generate a document. MiMo buffers tool-call arguments
// server-side and flushes them in one delayed burst (no incremental deltas), so a
// large document argument goes silent for far longer than the normal idle window —
// which would falsely trip the watchdog mid-generation (measured ~82s silent for a
// ~10KB spec). Widen to the document timeout and let the coarse total deadline
// backstop a genuine hang. Non-document turns keep the normal window: their tool
// arguments are small and stream promptly.
func (c *Client) toolCallIdleTimeout(tools []Tool) time.Duration {
	if c.idleTimeout > 0 && hasDocumentGenerationTool(tools) && documentToolTimeout > c.idleTimeout {
		return documentToolTimeout
	}
	return c.idleTimeout
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
