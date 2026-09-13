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
// context-window headroom for the prompt. (MiMo 2.5 Pro's real input window is
// materially smaller than the 131072 once noted here — do not size budgets
// against that figure; it was never verified against the deployment.)
const documentToolMaxCompletionTokens = 32768

// documentToolTimeout gives model turns that are serializing complete document
// payloads enough wall-clock time to reach the tool call. It is also the idle
// bound once a tool call is underway on such a turn (see ChatRequest.
// ToolCallIdleTimeout in llmwire): MiMo buffers the whole argument server-side
// and flushes it in one burst, ~82s of silence measured for a ~10KB spec.
const documentToolTimeout = 5 * time.Minute

// Hardcoded MiMo model selection. Loom targets MiMo specifically and is no
// longer model-configurable: textModel handles normal (text-only) turns, and
// visionModel (the omnimodal non-Pro variant) is used only for turns that carry
// image input — mimo-v2.5-pro is text-only and 404s on any image_url part.
//
// shortGateModel is the same non-Pro deployment, named separately because it is
// picked for a different reason: the short gates (see shortGate) need a fast
// answer, not a deep one, and the non-Pro variant responds sooner. Keeping the
// names apart means a future change to either use — a Pro that accepts images,
// a dedicated small model — moves one without silently moving the other.
//
// Not "the non-reasoning model": mimo-v2.5 is the same reasoning family as Pro
// and thinks when asked to. The gates suppress that per request (see
// llmwire.ReasoningOff) and route here because this deployment queues less,
// not because it cannot reason.
//
// These are wire ids llmwire resolves against its profile registry; the
// profile ships the host, and the key comes from LLMWIRE_MIMO_API_KEY, the
// variable the profiles' provider names.
const (
	textModel      = "mimo-v2.5-pro"
	visionModel    = "mimo-v2.5"
	shortGateModel = "mimo-v2.5"
)

// DefaultReasoningEffort is the reasoning depth used when a turn carries no
// per-request choice. Exported so the httpapi layer normalizes to the same value
// the llm client falls back to, keeping the default defined in exactly one place.
const DefaultReasoningEffort = "high"

// Config holds the chat client settings loom owns. The endpoint is llmwire's:
// BaseURL is an explicit override for a test fake or a stand-in endpoint and
// bypasses the environment entirely (no key is sent unless APIKey is set too).
type Config struct {
	BaseURL             string
	APIKey              string
	MaxCompletionTokens int
	// Timeout is the whole-call cap for a streamed turn. Zero takes
	// llmwire's default.
	Timeout time.Duration
	// IdleTimeout aborts a stream when no data frame arrives within the window.
	// It also bounds the wait for response headers: MiMo Pro queues, and loom
	// allowed this long before the first byte before the wire moved to llmwire.
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

type MessageContentPart struct {
	Type     string
	Text     string
	ImageURL *MessageImageURL
}

type MessageImageURL struct {
	URL string
}

// Client calls the MiMo chat completion endpoints through llmwire.
type Client struct {
	// wire is the one llmwire client every call goes through. One, not one per
	// call: it presents as opencode, and that identity carries a session id
	// llmwire mints and rotates itself.
	wire                *llmwire.Client
	model               string
	visionModel         string
	shortGateModel      string
	reasoningEffort     string
	maxCompletionTokens int
	timeout             time.Duration
}

// NewClient builds the chat client. The error is a missing LLMWIRE_MIMO_API_KEY,
// named, unless cfg.BaseURL wires a test endpoint itself.
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
	wire, err := llmwire.FromEnv(textModel, llmwire.Config{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		// Presents as the opencode client: its User-Agent and the session
		// header pair. The MiMo token plan is sold as that client's backend
		// and treats a neutral User-Agent as a bot.
		EmulateOpenCode: true,
		HeaderTimeout:   idleTimeout,
		IdleTimeout:     idleTimeout,
		CallTimeout:     callTimeout,
		HTTPClient:      httpClient,
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		wire:                wire,
		model:               textModel,
		visionModel:         visionModel,
		shortGateModel:      shortGateModel,
		reasoningEffort:     DefaultReasoningEffort,
		maxCompletionTokens: maxCompletionTokens,
		timeout:             cfg.Timeout,
	}, nil
}

// ModelSummary describes the hardcoded chat models for the startup capability line.
func ModelSummary() string {
	return textModel + " (text) / " + visionModel + " (vision)"
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

// resolveReasoningEffort picks the reasoning depth for a turn: the per-request
// value carried on the context (set by the httpapi layer from the composer
// selection) when present, else the client's configured default. Utility calls
// (titles, classifiers) never set the metadata field, so they keep the default.
func (c *Client) resolveReasoningEffort(ctx context.Context) string {
	if effort := inferenceMetadataFromContext(ctx).ReasoningEffort; effort != "" {
		return effort
	}
	return c.reasoningEffort
}

// completion is what a non-streaming helper call returned. Empty is the
// well-formed reply with no choices the endpoint emits when it drops a request;
// every gate degrades to a usable value on it rather than failing the turn.
type completion struct {
	Content      string
	FinishReason string
	Usage        TokenUsage
	Empty        bool
}

// complete runs one non-streaming call with thinking turned off via MiMo's
// native {"thinking":{"type":"disabled"}} and a caller-chosen completion-token
// cap. Default thinking makes MiMo overthink a trivial summarization and even
// echo its internal "reasoning>/response>" channel format as literal text
// instead of a clean title — besides burning ~1k reasoning tokens per call.
//
// The logging is done here for every helper: the completed line with the
// usage, or the failed line with the error. decided, when set, adds the
// caller's reading of the reply to the completed line — what a gate concluded
// is the one attribute that tells a mis-route from a correct one in the logs.
func (c *Client) complete(ctx context.Context, model string, messages []Message, maxTokens int, decided func(completion) []slog.Attr) (completion, error) {
	start := time.Now()
	resp, warnings, err := c.wire.Chat(ctx, llmwire.ChatRequest{
		Model:     model,
		Messages:  toWireMessages(messages),
		Reasoning: llmwire.ReasoningOff(),
		MaxTokens: &maxTokens,
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
	if !priced {
		noteUnpriced(ctx, model)
	}
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
// the two title generators. On top of disabled thinking it routes to
// shortGateModel, the non-Pro variant, which responds sooner (measured against
// a Pro that spent 78s queueing on a 64-token routing call).
//
// Deliberately NOT used by anything that writes prose a reader keeps: the forced
// final answer disables thinking too (see InferenceMetadata.SuppressThinking) but
// is a synthesis over gathered research and stays on the Pro model, as do project
// memory and the project description. The bar is that the answer is a label, an
// id or a handful of words nobody reads as prose — not that the deployment
// cannot reason, which is false; mimo-v2.5 reasons like Pro when asked to.
func (c *Client) shortGate(ctx context.Context, messages []Message, maxTokens int, decided func(completion) []slog.Attr) (completion, error) {
	return c.complete(ctx, c.shortGateModel, messages, maxTokens, decided)
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
