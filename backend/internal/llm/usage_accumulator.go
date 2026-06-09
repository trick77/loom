package llm

import (
	"context"
	"sync"
)

type usageAccumulatorKey struct{}

// UsageAccumulator sums TokenUsage across every model call made while serving a
// single request: the main answer turn, every tool round, and background helper
// calls such as the reasoning-abstract and thread-title generations. It is safe
// for concurrent use so background title goroutines can record into it while the
// answer streams. Attach one to the request context with WithUsageAccumulator;
// every completed call records into it via RecordUsage.
type UsageAccumulator struct {
	mu    sync.Mutex
	usage TokenUsage
}

func NewUsageAccumulator() *UsageAccumulator {
	return &UsageAccumulator{}
}

func (a *UsageAccumulator) add(u TokenUsage) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.usage.PromptTokens += u.PromptTokens
	a.usage.CompletionTokens += u.CompletionTokens
	a.usage.TotalTokens += u.TotalTokens
	a.usage.PromptTokensDetails.CachedTokens += u.PromptTokensDetails.CachedTokens
	a.usage.CompletionTokenDetails.ReasoningTokens += u.CompletionTokenDetails.ReasoningTokens
}

// Total returns the usage summed so far. Safe to call concurrently.
func (a *UsageAccumulator) Total() TokenUsage {
	if a == nil {
		return TokenUsage{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage
}

// WithUsageAccumulator returns a context carrying acc so that downstream model
// calls record their usage into it.
func WithUsageAccumulator(ctx context.Context, acc *UsageAccumulator) context.Context {
	return context.WithValue(ctx, usageAccumulatorKey{}, acc)
}

// RecordUsage adds one completed call's usage to the accumulator on ctx, if any.
// It is a no-op when no accumulator is attached, so call sites need not check.
func RecordUsage(ctx context.Context, usage TokenUsage) {
	if acc, _ := ctx.Value(usageAccumulatorKey{}).(*UsageAccumulator); acc != nil {
		acc.add(usage)
	}
}
