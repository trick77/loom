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
	// costNanoUSD sums the priced calls only; priced records whether any call
	// was priced, so a turn whose every call was unpriced reports "unknown"
	// rather than zero.
	costNanoUSD int64
	priced      bool
	// rolledUpNanoUSD is spend that belongs to the turn but is already in the
	// user's lifetime totals by another path (the RAG query embedding). It
	// counts in TurnCost, the figure the thread's Σ sums, and never in Cost,
	// which feeds the lifetime rollup.
	rolledUpNanoUSD int64
}

// NewUsageAccumulator creates a new accumulator for summing token usage across multiple model calls.
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

func (a *UsageAccumulator) addCost(nanoUSD int64, priced bool) {
	if a == nil || !priced {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.costNanoUSD += nanoUSD
	a.priced = true
}

// Cost returns the summed cost of the priced calls so far and whether any call
// was priced. Safe to call concurrently.
func (a *UsageAccumulator) Cost() (nanoUSD int64, priced bool) {
	if a == nil {
		return 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.costNanoUSD, a.priced
}

// TurnCost is everything the turn spent, the rolled-up calls included: the
// figure a message carries into the thread's Σ. priced is true when any call
// had a rate. Safe to call concurrently.
func (a *UsageAccumulator) TurnCost() (nanoUSD int64, priced bool) {
	if a == nil {
		return 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.costNanoUSD + a.rolledUpNanoUSD, a.priced || a.rolledUpNanoUSD > 0
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

// RecordCost adds one completed call's cost to the accumulator on ctx, if any.
// An unpriced call adds nothing.
func RecordCost(ctx context.Context, nanoUSD int64, priced bool) {
	if acc, _ := ctx.Value(usageAccumulatorKey{}).(*UsageAccumulator); acc != nil {
		acc.addCost(nanoUSD, priced)
	}
}

// RecordRolledUpCost adds the priced cost of a call whose spend the caller
// already added to the user's lifetime totals itself, so the turn's figure
// counts it without the lifetime rollup counting it twice. No-op without an
// accumulator on ctx or for a zero cost.
func RecordRolledUpCost(ctx context.Context, nanoUSD int64) {
	acc, _ := ctx.Value(usageAccumulatorKey{}).(*UsageAccumulator)
	if acc == nil || nanoUSD <= 0 {
		return
	}
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.rolledUpNanoUSD += nanoUSD
}
