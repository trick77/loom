package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/inference"
)

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
// context-window headroom for the prompt.
const documentToolMaxCompletionTokens = 32768

// documentToolTimeout gives model turns that are serializing complete document
// payloads enough wall-clock time to reach the tool call. It is also the idle
// bound once a tool call is underway on such a turn (see ChatRequest.
// ToolCallIdleTimeout in llmwire): an endpoint that buffers the whole argument
// server-side stays silent for as long as it takes to write it.
const documentToolTimeout = 5 * time.Minute

// Config holds the chat client settings loom owns. The endpoint is llmwire's:
// BaseURL is an explicit override for a test fake or a stand-in endpoint and
// bypasses the environment entirely (no key is sent unless APIKey is set too).
type Config struct {
	// Models names the model per role; see Roles.
	Models Roles
	// Registry resolves the models; nil is llmwire's default registry. Tests
	// pass a synthetic one.
	Registry            *llmwire.Registry
	BaseURL             string
	APIKey              string
	MaxCompletionTokens int
	// Timeout is the whole-call cap for a streamed turn. Zero takes
	// llmwire's default.
	Timeout time.Duration
	// IdleTimeout aborts a stream when no data frame arrives within the window.
	// It also bounds the wait for response headers, so a queueing endpoint
	// trips it before the first byte.
	// Zero disables the watchdog: the whole-call cap is then the only bound.
	IdleTimeout time.Duration
	// ResponseLogDir, when set, spools every raw response to that directory
	// (llmwire.SpoolTransport); incognito turns are never written.
	ResponseLogDir string
}

// Message is one chat message in loom's own shape; wire.go converts it to
// llmwire's before a request goes out.
type Message struct {
	Role             string
	Content          string
	ContentParts     []MessageContentPart
	ReasoningContent string
	ToolCalls        []ToolCall
	ToolCallID       string
}

// MessageContentPart is a single content part in a chat message (text or image).
type MessageContentPart struct {
	Type     string
	Text     string
	ImageURL *MessageImageURL
}

// MessageImageURL holds the URL of an image in a message content part.
type MessageImageURL struct {
	URL string
}

// Client calls the chat models through llmwire.
type Client struct {
	// wire is the one llmwire client every call goes through, routing each
	// request to its model's provider. One, not one per call: when it presents
	// as opencode (LLMWIRE_EMULATE_OPENCODE), that identity carries a session
	// id llmwire mints and rotates itself.
	wire                *llmwire.Client
	model               string
	visionModel         string
	gateModel           string
	maxCompletionTokens int
	timeout             time.Duration
}

// NewClient builds the chat client for cfg.Models. The error is an unusable
// role (see ResolveRoles) or a missing key variable, named, unless
// cfg.BaseURL wires a test endpoint itself.
func NewClient(cfg Config, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if cfg.ResponseLogDir != "" {
		// A copy, so the spool never lands on a client shared with anything
		// else. Incognito turns are ephemeral by contract and are skipped.
		hc := *httpClient
		hc.Transport = llmwire.NewSpoolTransport(cfg.ResponseLogDir, hc.Transport, func(r *http.Request) bool {
			return inference.MetadataFromContext(r.Context()).Incognito
		})
		httpClient = &hc
	}
	maxCompletionTokens := cfg.MaxCompletionTokens
	if maxCompletionTokens <= 0 {
		maxCompletionTokens = defaultMaxCompletionTokens
	}
	callTimeout := cfg.Timeout
	if callTimeout > 0 && callTimeout < documentToolTimeout {
		// The document turns need the wider cap; the narrower one is applied
		// per call in StreamChatWithTools for every other turn.
		callTimeout = documentToolTimeout
	}
	idleTimeout := cfg.IdleTimeout
	if idleTimeout <= 0 {
		// llmwire has no off switch for its stream bounds (zero means its
		// defaults), so "disabled" is spelled as a window as wide as the call.
		idleTimeout = callTimeout
		if idleTimeout <= 0 {
			idleTimeout = llmwire.DefaultCallTimeout
		}
	}
	resolved, err := ResolveRoles(cfg.Registry, cfg.Models)
	if err != nil {
		return nil, err
	}
	roles := resolved.Roles
	wire, err := llmwire.FromEnvModels(llmwire.Config{
		BaseURL:       cfg.BaseURL,
		APIKey:        cfg.APIKey,
		HeaderTimeout: idleTimeout,
		IdleTimeout:   idleTimeout,
		CallTimeout:   callTimeout,
		HTTPClient:    httpClient,
		Registry:      cfg.Registry,
	}, roles.ids()...)
	if err != nil {
		return nil, err
	}
	return &Client{
		wire:                wire,
		model:               roles.Chat,
		visionModel:         roles.Vision,
		gateModel:           roles.Gate,
		maxCompletionTokens: maxCompletionTokens,
		timeout:             cfg.Timeout,
	}, nil
}

// modelForMessages selects the model for a streamed request: the vision model
// when the payload carries an image, the chat model otherwise. This is the
// single routing decision; callers thread the returned name through the
// request body and into StreamResult.Model so the persisted/observed model
// reflects what actually ran.
func (c *Client) modelForMessages(messages []Message) string {
	for _, m := range toWireMessages(messages) {
		if m.HasImage() {
			return c.visionModel
		}
	}
	return c.model
}

// utilityMaxCompletionTokens caps the answer of secondary helper calls
// (titles). A clean title is a handful of tokens; the cap is a guard so a
// misbehaving turn can never run long. Sized with headroom for an 8-word gerund
// title — a call that still hits the cap is treated as truncated and discarded
// (see title decoders). It is an answer budget: llmwire adds the model's
// reasoning room on top.
const utilityMaxCompletionTokens = 32

// completion is what a non-streaming helper call returned. Empty is the
// well-formed reply with no choices the endpoint emits when it drops a request;
// every gate degrades to a usable value on it rather than failing the turn.
type completion struct {
	Content      string
	FinishReason string
	Usage        TokenUsage
	Empty        bool
}

// complete runs one non-streaming helper call: model, the reasoning the
// caller asks for, and answerTokens as the cap on the visible answer (llmwire
// adds the model's reasoning room on top).
//
// The logging is done here for every helper: the completed line with the
// usage, or the failed line with the error. decided, when set, adds the
// caller's reading of the reply to the completed line — what a gate concluded
// is the one attribute that tells a mis-route from a correct one in the logs.
func (c *Client) complete(ctx context.Context, model string, messages []Message, answerTokens int, reasoning llmwire.ReasoningRequest, decided func(completion) []slog.Attr) (completion, error) {
	start := time.Now()
	resp, warnings, err := c.wire.Chat(ctx, llmwire.ChatRequest{
		Model:           model,
		Messages:        toWireMessages(messages),
		Reasoning:       reasoning,
		MaxAnswerTokens: &answerTokens,
	})
	logWarnings(ctx, model, warnings)
	if err != nil {
		if errors.Is(err, llmwire.ErrResponseShape) {
			// No choices and no error object: the endpoint dropped the request.
			logInferenceCompleted(ctx, model, time.Since(start), TokenUsage{}, "")
			return completion{Empty: true}, nil
		}
		err = chatError(err)
		logInferenceFailed(ctx, model, time.Since(start), err)
		return completion{}, err
	}
	usage := usageFromWire(resp.Usage)
	cost, priced := costFromWire(resp.Usage)
	reply := completion{Content: resp.Content, FinishReason: resp.FinishReason, Usage: usage}
	var extra []slog.Attr
	if decided != nil {
		extra = decided(reply)
	}
	observeInference(ctx, model, durationOr(resp.Timing.Total, start), usage, resp.FinishReason, extra...)
	RecordCost(ctx, cost, priced)
	return reply, nil
}

// shortGate runs a helper call that needs a fast answer rather than a deep one
// — the short gates a turn blocks on: image intent, thread classification, and
// the two title generators. They run on the gate model with the least
// reasoning it allows: the answer is a label, an id or a handful of words, and
// the turn waits on them before its first token.
//
// Deliberately NOT used by anything that writes prose a reader keeps (the
// forced final answer, project memory and description): on some models the
// least reasoning is none at all, which costs correctness there.
func (c *Client) shortGate(ctx context.Context, messages []Message, answerTokens int, decided func(completion) []slog.Attr) (completion, error) {
	return c.complete(ctx, c.gateModel, messages, answerTokens, llmwire.ReasoningMinimal(), decided)
}

// prose runs a helper call that writes text a reader keeps (project memory,
// project description) on the chat model at its balanced reasoning.
func (c *Client) prose(ctx context.Context, messages []Message, answerTokens int) (completion, error) {
	return c.complete(ctx, c.model, messages, answerTokens, llmwire.ReasoningBalanced(), nil)
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
// legitimately needs the room — and intent cannot be known up front. The idle
// watchdog catches a stalled reasoning/content phase in seconds; but once a
// document tool call is underway it widens (see toolCallIdleTimeout), so this
// coarse deadline is the real backstop for that phase.
func (c *Client) timeoutForTools(tools []Tool) time.Duration {
	if c.timeout == 0 || !hasDocumentGenerationTool(tools) || c.timeout >= documentToolTimeout {
		return c.timeout
	}
	return documentToolTimeout
}

// toolCallIdleTimeout is the idle window to apply once a tool call is underway in
// a turn that can generate a document. An endpoint that buffers tool-call
// arguments server-side flushes them in one delayed burst (no incremental
// deltas), so a large document argument goes silent for far longer than the
// normal idle window — which would falsely trip the watchdog mid-generation
// (measured on MiMo: ~82s silent for a ~10KB spec; not yet measured on
// glm-5.3-flash, so the window stays until it is). Widen to the document timeout and let the coarse total deadline
// backstop a genuine hang. Non-document turns keep the normal window: their tool
// arguments are small and stream promptly. Zero means "no change" to llmwire.
func toolCallIdleTimeout(tools []Tool) time.Duration {
	if hasDocumentGenerationTool(tools) {
		return documentToolTimeout
	}
	return 0
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
