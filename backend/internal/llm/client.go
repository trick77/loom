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
// ToolCallIdleTimeout in llmwire): MiMo buffered the whole argument server-side
// and flushed it in one burst, ~82s of silence measured for a ~10KB spec.
const documentToolTimeout = 5 * time.Minute

// Hardcoded chat model: glm-5.3-flash on Z.ai's general host, the model peeq
// runs. It replaced MiMo because MiMo was too slow for interactive chat;
// llmwire's profile notes glm-5.3-flash at 1-5s per call against 25-64s on the
// MiMo lane it measured beside it. Loom is not model-configurable.
//
// One model serves every role. Vision is vendor documentation only in the
// profile (the image probe ran against MiMo), so an image turn is the first
// thing to check if image answers go wrong. The four constants stay separate
// names rather than one so a future split — a cheaper gate model, a model that
// earns its keep on synthesis — moves one use without silently moving the
// others.
//
// These are wire ids llmwire resolves against its profile registry; the
// profile ships the host, and the key comes from LLMWIRE_ZAI_API_KEY, the
// variable the profile's provider names. A GLM Coding Plan key does not work:
// that host is restricted to Z.ai's own tools.
const (
	textModel      = "glm-5.3-flash"
	visionModel    = "glm-5.3-flash"
	shortGateModel = "glm-5.3-flash"
	proseModel     = "glm-5.3-flash"
)

// Reasoning is steered by effort level only. glm-5.3-flash always thinks: the
// disable toggle is refused (400, code 1210) and llmwire rejects
// ReasoningOff() for it before the request goes out. It accepts exactly low,
// high and max, and an absent level means the vendor default max.
//
// turnReasoningEffort is what a normal streamed turn asks for. Not max: peeq
// measured the same job at 12.8s on high against 69.9s on max, and speed is
// why loom moved to this model. helperReasoningEffort is for the helper calls
// (see complete) and the forced final answer (see
// InferenceMetadata.SuppressThinking) — the sites that turned thinking off on
// MiMo. low is the nearest the model allows; peeq measured it at 5 reasoning
// tokens against 43 on high.
const (
	turnReasoningEffort   = "high"
	helperReasoningEffort = "low"
)

// helperReasoningHeadroom is added to every helper call's answer cap (see
// complete): the model always thinks and the reasoning counts against
// max_tokens, so a cap sized for a title alone is spent thinking and the
// reply comes back truncated. peeq measured ~53 reasoning tokens at low on a
// short gate; 1024 leaves wide room. Only generated tokens are billed, and a
// runaway reply still hits the cap and is discarded as truncated.
const helperReasoningHeadroom = 1024

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

// Client calls the Z.ai chat completion endpoint through llmwire.
type Client struct {
	// wire is the one llmwire client every call goes through. One, not one per
	// call: when it presents as opencode (LLMWIRE_EMULATE_OPENCODE), that
	// identity carries a session id llmwire mints and rotates itself.
	wire                *llmwire.Client
	model               string
	visionModel         string
	shortGateModel      string
	proseModel          string
	maxCompletionTokens int
	timeout             time.Duration
}

// NewClient builds the chat client. The error is a missing LLMWIRE_ZAI_API_KEY,
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
		BaseURL:       cfg.BaseURL,
		APIKey:        cfg.APIKey,
		HeaderTimeout: idleTimeout,
		IdleTimeout:   idleTimeout,
		CallTimeout:   callTimeout,
		HTTPClient:    httpClient,
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		wire:                wire,
		model:               textModel,
		visionModel:         visionModel,
		shortGateModel:      shortGateModel,
		proseModel:          proseModel,
		maxCompletionTokens: maxCompletionTokens,
		timeout:             cfg.Timeout,
	}, nil
}

// ModelSummary describes the hardcoded chat model for the startup capability
// line. One name, because one model now serves text, vision and the short
// gates. If a future change splits the constants again this wants the roles
// spelled out; a conditional here is dead code while they agree, which go vet
// flags as a suspect constant comparison.
func ModelSummary() string {
	return textModel
}

// modelForMessages selects the chat model for a request. This is the single
// routing decision; callers thread the returned name through the request body
// and into StreamResult.Model so the persisted/observed model reflects what
// actually ran.
//
// thinkingOff (the forced final answer) wins over the image check, so that
// turn stays on proseModel even when the research it summarizes carried an
// image. Every constant names the same model today, so the order only matters
// once they split again.
func (c *Client) modelForMessages(messages []Message, thinkingOff bool) string {
	if thinkingOff {
		return c.proseModel
	}
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

// completion is what a non-streaming helper call returned. Empty is the
// well-formed reply with no choices the endpoint emits when it drops a request;
// every gate degrades to a usable value on it rather than failing the turn.
type completion struct {
	Content      string
	FinishReason string
	Usage        TokenUsage
	Empty        bool
}

// complete runs one non-streaming call at helperReasoningEffort and a
// caller-chosen completion-token cap. Thinking cannot be switched off on this
// model, so the shallowest level is the lever: deep thinking on a trivial
// summarization burns the cap before the answer and adds seconds to a gate the
// turn waits on.
//
// The logging is done here for every helper: the completed line with the
// usage, or the failed line with the error. decided, when set, adds the
// caller's reading of the reply to the completed line — what a gate concluded
// is the one attribute that tells a mis-route from a correct one in the logs.
func (c *Client) complete(ctx context.Context, model string, messages []Message, maxTokens int, decided func(completion) []slog.Attr) (completion, error) {
	start := time.Now()
	// maxTokens is sized for the answer; the reasoning gets its own room.
	maxTokens += helperReasoningHeadroom
	resp, warnings, err := c.wire.Chat(ctx, llmwire.ChatRequest{
		Model:     model,
		Messages:  toWireMessages(messages),
		Reasoning: llmwire.ReasoningEffort(helperReasoningEffort),
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
// the two title generators. It routes to shortGateModel on top of the helper
// effort.
//
// That routing is currently a no-op: every chat constant is glm-5.3-flash, so
// a gate and a main turn hit the same deployment. The seam is kept because the
// gates are the calls a turn WAITS on before its first token — three of them
// serialized, each on a 30s bound — so they are where a cheaper or faster model
// would be pointed first, and pointing it wants one constant to move, not a
// grep through the call sites.
//
// Deliberately NOT used by anything that writes prose a reader keeps: the
// forced final answer runs at the helper effort too (see
// InferenceMetadata.SuppressThinking) but is a synthesis over gathered
// research, as are project memory and the project description. The bar is that
// the answer is a label, an id or a handful of words nobody reads as prose.
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
