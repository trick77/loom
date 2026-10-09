package turntest

import (
	"context"

	"github.com/trick77/loom/internal/usage"
)

// UsageStore is a usage store that discards writes and reports Totals.
type UsageStore struct{ Totals usage.Totals }

// RecordingUsageStore captures the lifetime token rollups a turn writes, so a
// test can assert which helper calls were still inside the accumulator when it
// was read.
type RecordingUsageStore struct {
	UsageStore
	Deltas []usage.TokenDelta
}

// AddTokens implements turn.UsageStore.
func (s *RecordingUsageStore) AddTokens(_ context.Context, _ string, delta usage.TokenDelta) error {
	s.Deltas = append(s.Deltas, delta)
	return nil
}

// AddTokens implements turn.UsageStore.
func (s UsageStore) AddTokens(context.Context, string, usage.TokenDelta) error { return nil }

// IncWebSearch implements turn.UsageStore.
func (s UsageStore) IncWebSearch(context.Context, string) error { return nil }

// IncWebFetch implements turn.UsageStore.
func (s UsageStore) IncWebFetch(context.Context, string) error { return nil }

// IncObscuraFetch implements turn.UsageStore.
func (s UsageStore) IncObscuraFetch(context.Context, string) error { return nil }

// IncImageGen implements turn.UsageStore.
func (s UsageStore) IncImageGen(context.Context, string) error { return nil }

// IncCodeRun implements turn.UsageStore.
func (s UsageStore) IncCodeRun(context.Context, string) error { return nil }

// IncThreadCreated implements turn.UsageStore.
func (s UsageStore) IncThreadCreated(context.Context, string) error { return nil }

// IncProjectCreated implements turn.UsageStore.
func (s UsageStore) IncProjectCreated(context.Context, string) error { return nil }

// Get implements turn.UsageStore.
func (s UsageStore) Get(context.Context, string) (usage.Totals, error) { return s.Totals, nil }
