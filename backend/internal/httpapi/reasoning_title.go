package httpapi

import (
	"context"
	"strings"
	"sync"

	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/sse"
)

// reasoningTitleTracker generates a short abstract title for each reasoning
// round in the background. Titles are emitted over SSE as they become ready and
// collected so they can be merged into the persisted activity trace. The zero
// value is not usable; build one with newReasoningTitleTracker.
type reasoningTitleTracker struct {
	s       *server
	stream  *sse.Writer
	ctx     context.Context
	inf     llm.InferenceMetadata
	wg      sync.WaitGroup
	mu      sync.Mutex
	titles  map[string]string // reasoning id -> title
	spawned map[string]bool   // reasoning id -> already generating
}

func newReasoningTitleTracker(s *server, stream *sse.Writer, ctx context.Context, inf llm.InferenceMetadata) *reasoningTitleTracker {
	return &reasoningTitleTracker{s: s, stream: stream, ctx: ctx, inf: inf, titles: map[string]string{}, spawned: map[string]bool{}}
}

// spawn kicks off a background title generation for one reasoning round. It is a
// no-op when there is no reasoning id or no reasoning content, and is idempotent
// per id so the mid-turn boundary and the post-turn fallback never double-fire.
// The caller must eventually call wait() before tearing down the stream.
func (t *reasoningTitleTracker) spawn(reasoningID, reasoning string) {
	if t == nil || reasoningID == "" || strings.TrimSpace(reasoning) == "" {
		return
	}
	t.mu.Lock()
	if t.spawned[reasoningID] {
		t.mu.Unlock()
		return
	}
	t.spawned[reasoningID] = true
	t.mu.Unlock()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		inf := t.inf
		inf.Purpose = "reasoning_title"
		title, err := t.s.llm.GenerateReasoningTitle(llm.WithInferenceMetadata(t.ctx, inf), reasoning)
		if err != nil || strings.TrimSpace(title) == "" {
			return
		}
		t.mu.Lock()
		t.titles[reasoningID] = title
		t.mu.Unlock()
		_ = sendSSEJSON(t.stream, "assistant_reasoning_title", reasoningTitleResponse{ID: reasoningID, Title: title})
	}()
}

// wait blocks until every spawned title goroutine has finished.
func (t *reasoningTitleTracker) wait() {
	if t == nil {
		return
	}
	t.wg.Wait()
}

// mergeInto stamps collected titles onto their matching reasoning events. Call
// after wait() so the persisted trace carries the titles.
func (t *reasoningTitleTracker) mergeInto(trace []activityTraceEvent) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range trace {
		if trace[i].Type != "reasoning" {
			continue
		}
		if title, ok := t.titles[trace[i].ID]; ok {
			trace[i].Title = title
		}
	}
}

type reasoningTitleResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// appendTraceAndSpawnTitle appends a turn's result to the trace and, when the
// turn produced reasoning, spawns a background title for it.
func (s *server) appendTraceAndSpawnTitle(t *reasoningTitleTracker, cur []activityTraceEvent, res llm.StreamResult) []activityTraceEvent {
	next, reasoningID := activityTraceFromResult(cur, res)
	t.spawn(reasoningID, res.ReasoningContent)
	return next
}
