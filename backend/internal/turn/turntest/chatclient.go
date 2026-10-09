package turntest

import (
	"context"
	"sync/atomic"

	"github.com/trick77/loom/internal/llm"
)

// ChatClient is a chat client that streams "Hello" (or StreamText) and returns
// canned helper replies; its fields gate, record or fail individual calls.
type ChatClient struct {
	Title               string
	TitleErr            error
	Category            string
	ReasoningTitle      string
	ReasoningTitlePanic bool
	// ReasoningTitleGate, when set, holds GenerateReasoningTitle open until it
	// is closed: a title call stuck on a dead upstream.
	ReasoningTitleGate chan struct{}
	// ReasoningTitleSeen, when set, receives the reasoning every
	// GenerateReasoningTitle call was given.
	ReasoningTitleSeen chan string
	// ReasoningDeltas, when set, streams the reasoning as these deltas in place
	// of reasoningText, pausing on reasoningHold (when set) after the first:
	// a model still thinking.
	ReasoningDeltas []string
	ReasoningHold   chan struct{}
	StreamPanic     bool
	// ClassifyGate, when set, holds ClassifyThread open until it is closed.
	ClassifyGate chan struct{}
	// ClassifyEntered, when set (buffered), is signalled as ClassifyThread starts.
	ClassifyEntered chan struct{}
	// ImageIntentGate, when set, holds ClassifyImageIntent open until it is closed.
	ImageIntentGate chan struct{}
	// WorkingTitle is the reply of GenerateWorkingTitle; workingTitleCost is
	// recorded as priced spend when non-zero.
	WorkingTitle     string
	WorkingTitleCost int64
	// WorkingTitleGate, when set, holds GenerateWorkingTitle open until it is
	// closed: a working-title call on a slow endpoint.
	WorkingTitleGate chan struct{}
	// MemoryEntered, memoryGate and memoryCalls let a test hold GenerateMemory
	// open and count how many callers got through.
	MemoryEntered chan struct{}
	MemoryGate    chan struct{}
	MemoryCalls   *atomic.Int32
	// MemoryPriors, when set, receives the prior memory passed to GenerateMemory.
	MemoryPriors        chan string
	History             *[]llm.Message
	StreamText          *string
	ReasoningText       string
	Usage               llm.TokenUsage
	TitleUsage          llm.TokenUsage
	ReasoningTitleUsage llm.TokenUsage
	// Cost, titleCost and reasoningTitleCost are recorded as priced nano-USD
	// next to the matching usage when non-zero.
	Cost               int64
	TitleCost          int64
	ReasoningTitleCost int64
	AfterStream        func()
	ProjectMemory      string
	EditedMemory       string
	ProjectDescription string
	// ProjectDescriptionCalls, when set, counts GenerateProjectDescription
	// invocations so a test can assert the in-memory guard short-circuited before
	// any (wasted) inference.
	ProjectDescriptionCalls *int
	// StreamErr, when set, makes StreamChatWithTools emit any reasoning then return
	// the error (no content), modelling a turn that fails/stalls mid-stream.
	StreamErr error
	// StreamErrCost is recorded as priced spend before streamErr is returned,
	// modelling rounds that were billed before the turn failed.
	StreamErrCost int64
	// StreamErrDelta is emitted as content before streamErr is returned.
	StreamErrDelta string
	// ImageIntent is the canned reply of the semantic image-routing gate. Its
	// zero value ({Action:""}) maps to ImageIntentNone, so tests that never touch
	// images get the non-image path for free.
	ImageIntent llm.ImageIntent
	// TitleAssistantSeen, when set, captures the assistant message the title gate
	// was given, so a test can assert the answer reaches it rather than the empty
	// string production used to pass.
	TitleAssistantSeen *string
}

// StreamChatResult streams "Hello" in two deltas and returns StreamText when
// set.
func (f ChatClient) StreamChatResult(_ context.Context, history []llm.Message, onDelta func(string) error) (llm.StreamResult, error) {
	if f.History != nil {
		*f.History = append((*f.History)[:0], history...)
	}
	if err := onDelta("Hel"); err != nil {
		return llm.StreamResult{}, err
	}
	if err := onDelta("lo"); err != nil {
		return llm.StreamResult{}, err
	}
	if f.AfterStream != nil {
		f.AfterStream()
	}
	if f.StreamText != nil {
		return llm.StreamResult{Content: *f.StreamText, Usage: f.Usage}, nil
	}
	return llm.StreamResult{Content: "Hello", Usage: f.Usage}, nil
}

// GenerateThreadTitle implements turn.ChatClient.
func (f ChatClient) GenerateThreadTitle(ctx context.Context, _, assistantMessage, _ string) (string, error) {
	if f.TitleAssistantSeen != nil {
		*f.TitleAssistantSeen = assistantMessage
	}
	if f.TitleErr != nil {
		return "", f.TitleErr
	}
	// Mirror the real client: a completed helper call records its usage into the
	// request accumulator on ctx.
	llm.RecordUsage(ctx, f.TitleUsage)
	if f.TitleCost > 0 {
		llm.RecordCost(ctx, f.TitleCost, true)
	}
	return f.Title, nil
}

// ClassifyThread implements turn.ChatClient.
func (f ChatClient) ClassifyThread(ctx context.Context, _ string) (string, error) {
	if f.ClassifyEntered != nil {
		select {
		case f.ClassifyEntered <- struct{}{}:
		default:
		}
	}
	if f.ClassifyGate != nil {
		select {
		case <-f.ClassifyGate:
		case <-ctx.Done():
		}
	}
	return f.Category, nil
}

// ClassifyImageIntent implements turn.ChatClient.
func (f ChatClient) ClassifyImageIntent(ctx context.Context, _ string, _, _ bool) (llm.ImageIntent, error) {
	if f.ImageIntentGate != nil {
		select {
		case <-f.ImageIntentGate:
		case <-ctx.Done():
		}
	}
	return f.ImageIntent, nil
}

// GenerateWorkingTitle implements turn.ChatClient.
func (f ChatClient) GenerateWorkingTitle(ctx context.Context, _, _ string) (string, error) {
	if f.WorkingTitleGate != nil {
		select {
		case <-f.WorkingTitleGate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.WorkingTitleCost > 0 {
		llm.RecordCost(ctx, f.WorkingTitleCost, true)
	}
	return f.WorkingTitle, nil
}

// GenerateReasoningTitle implements turn.ChatClient.
func (f ChatClient) GenerateReasoningTitle(ctx context.Context, reasoning, _ string) (string, error) {
	if f.ReasoningTitlePanic {
		panic("reasoning title exploded")
	}
	if f.ReasoningTitleSeen != nil {
		f.ReasoningTitleSeen <- reasoning
	}
	if f.ReasoningTitleGate != nil {
		<-f.ReasoningTitleGate
	}
	llm.RecordUsage(ctx, f.ReasoningTitleUsage)
	if f.ReasoningTitleCost > 0 {
		llm.RecordCost(ctx, f.ReasoningTitleCost, true)
	}
	return f.ReasoningTitle, nil
}

// GenerateMemory fakes the memory refresh, which runs outside a turn.
func (f ChatClient) GenerateMemory(_ context.Context, _, prior, _, _, _, _ string) (string, error) {
	if f.MemoryPriors != nil {
		f.MemoryPriors <- prior
	}
	if f.MemoryCalls != nil {
		f.MemoryCalls.Add(1)
	}
	if f.MemoryEntered != nil {
		f.MemoryEntered <- struct{}{}
	}
	if f.MemoryGate != nil {
		<-f.MemoryGate
	}
	return f.ProjectMemory, nil
}

// ApplyMemoryEdit fakes a user's memory edit, which runs outside a turn.
func (f ChatClient) ApplyMemoryEdit(_ context.Context, _, _, _, _, _ string) (string, error) {
	return f.EditedMemory, nil
}

// GenerateProjectDescription fakes the project description refresh, which runs
// outside a turn.
func (f ChatClient) GenerateProjectDescription(_ context.Context, _ string, _ []string, _ string) (string, error) {
	if f.ProjectDescriptionCalls != nil {
		*f.ProjectDescriptionCalls++
	}
	return f.ProjectDescription, nil
}

// StreamChatWithTools implements turn.ChatClient.
func (f ChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, _ []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	if f.StreamPanic {
		panic("stream exploded")
	}
	if f.History != nil {
		*f.History = append((*f.History)[:0], history...)
	}
	if f.ReasoningText != "" && onEvent != nil {
		if err := onEvent(llm.StreamEvent{ReasoningDelta: f.ReasoningText}); err != nil {
			return llm.StreamResult{}, err
		}
	}
	for i, delta := range f.ReasoningDeltas {
		if err := onEvent(llm.StreamEvent{ReasoningDelta: delta}); err != nil {
			return llm.StreamResult{}, err
		}
		if i == 0 && f.ReasoningHold != nil {
			select {
			case <-f.ReasoningHold:
			case <-ctx.Done():
				return llm.StreamResult{}, ctx.Err()
			}
		}
	}
	if f.StreamErr != nil {
		// streamErrDelta: some prose streamed before the failure, which is the
		// reasoning->content boundary where a reasoning title starts.
		if f.StreamErrDelta != "" && onEvent != nil {
			if err := onEvent(llm.StreamEvent{Delta: f.StreamErrDelta}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		if f.StreamErrCost > 0 {
			llm.RecordCost(ctx, f.StreamErrCost, true)
		}
		return llm.StreamResult{ReasoningContent: f.ReasoningText}, f.StreamErr
	}
	content := "Hello"
	if f.StreamText != nil {
		content = *f.StreamText
	}
	if onEvent != nil {
		if f.StreamText != nil {
			if err := onEvent(llm.StreamEvent{Delta: content}); err != nil {
				return llm.StreamResult{}, err
			}
		} else {
			for _, delta := range []string{"Hel", "lo"} {
				if err := onEvent(llm.StreamEvent{Delta: delta}); err != nil {
					return llm.StreamResult{}, err
				}
			}
		}
	}
	if f.AfterStream != nil {
		f.AfterStream()
	}
	llm.RecordUsage(ctx, f.Usage)
	if f.Cost > 0 {
		llm.RecordCost(ctx, f.Cost, true)
	}
	return llm.StreamResult{Content: content, ReasoningContent: f.ReasoningText, Usage: f.Usage, CostNanoUSD: f.Cost, CostPriced: f.Cost > 0}, nil
}

// ToolChatClient is a chat client that replays Results, one per tool-enabled
// round, and answers Plain when called without tools. It records every round's
// history and tools.
type ToolChatClient struct {
	Results        []llm.StreamResult
	Histories      [][]llm.Message
	Tools          [][]llm.Tool
	Plain          string
	PlainErr       error
	ClassifyResult string
	ImageIntent    llm.ImageIntent
	TitleResult    string
	TitleFor       func(reasoning string) string
}

// StreamChatResult answers a tool-free round with Plain and PlainErr.
func (f *ToolChatClient) StreamChatResult(context.Context, []llm.Message, func(string) error) (llm.StreamResult, error) {
	if f.PlainErr != nil {
		return llm.StreamResult{Content: f.Plain}, f.PlainErr
	}
	if f.Plain == "" {
		return llm.StreamResult{}, nil
	}
	return llm.StreamResult{Content: f.Plain}, nil
}

// StreamChatWithTools implements turn.ChatClient.
func (f *ToolChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, tools []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	f.Histories = append(f.Histories, append([]llm.Message(nil), history...))
	f.Tools = append(f.Tools, append([]llm.Tool(nil), tools...))
	if len(tools) == 0 {
		result, err := f.StreamChatResult(context.Background(), history, nil)
		if err != nil {
			return llm.StreamResult{}, err
		}
		if onEvent != nil {
			if result.ReasoningContent != "" {
				if err := onEvent(llm.StreamEvent{ReasoningDelta: result.ReasoningContent}); err != nil {
					return llm.StreamResult{}, err
				}
			}
			if result.Content != "" {
				if err := onEvent(llm.StreamEvent{Delta: result.Content}); err != nil {
					return llm.StreamResult{}, err
				}
			}
		}
		llm.RecordUsage(ctx, result.Usage)
		return result, nil
	}
	result := f.Results[0]
	f.Results = f.Results[1:]
	if onEvent != nil {
		if result.ReasoningContent != "" {
			if err := onEvent(llm.StreamEvent{ReasoningDelta: result.ReasoningContent}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		// Mirror the real client: announce a pending tool call before the parsed
		// call surfaces, so handler/SSE behavior is exercised faithfully.
		if len(result.ToolCalls) > 0 {
			if err := onEvent(llm.StreamEvent{ToolPending: true}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		for _, call := range result.ToolCalls {
			if err := onEvent(llm.StreamEvent{ToolCall: call}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		if result.Content != "" {
			if err := onEvent(llm.StreamEvent{Delta: result.Content}); err != nil {
				return llm.StreamResult{}, err
			}
		}
	}
	llm.RecordUsage(ctx, result.Usage)
	return result, nil
}

// GenerateThreadTitle implements turn.ChatClient.
func (f *ToolChatClient) GenerateThreadTitle(context.Context, string, string, string) (string, error) {
	return f.TitleResult, nil
}

// ClassifyThread implements turn.ChatClient.
func (f *ToolChatClient) ClassifyThread(context.Context, string) (string, error) {
	return f.ClassifyResult, nil
}

// ClassifyImageIntent implements turn.ChatClient.
func (f *ToolChatClient) ClassifyImageIntent(context.Context, string, bool, bool) (llm.ImageIntent, error) {
	return f.ImageIntent, nil
}

// GenerateReasoningTitle implements turn.ChatClient.
func (f *ToolChatClient) GenerateReasoningTitle(_ context.Context, reasoning, _ string) (string, error) {
	if f.TitleFor != nil {
		return f.TitleFor(reasoning), nil
	}
	return "", nil
}

// GenerateWorkingTitle implements turn.ChatClient.
func (f *ToolChatClient) GenerateWorkingTitle(context.Context, string, string) (string, error) {
	return "", nil
}

// GenerateMemory fakes the memory refresh, which runs outside a turn.
func (f *ToolChatClient) GenerateMemory(_ context.Context, _, _, _, _, _, _ string) (string, error) {
	return "", nil
}

// ApplyMemoryEdit fakes a user's memory edit, which runs outside a turn.
func (f *ToolChatClient) ApplyMemoryEdit(_ context.Context, _, _, _, _, _ string) (string, error) {
	return "", nil
}

// GenerateProjectDescription fakes the project description refresh, which runs
// outside a turn.
func (f *ToolChatClient) GenerateProjectDescription(context.Context, string, []string, string) (string, error) {
	return "", nil
}
