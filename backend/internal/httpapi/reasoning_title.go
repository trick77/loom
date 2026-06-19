package httpapi

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

// reasoningTitleTimeout bounds a single background title call. wait() blocks the
// final assistant_message on outstanding title goroutines and the shared HTTP
// client has no timeout, so without this a hung title call would stall delivery.
const reasoningTitleTimeout = 10 * time.Second

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
		// Bound the call so a hung title request can never delay delivery of the
		// final answer: wait() blocks assistant_message on this goroutine, and the
		// shared HTTP client has no timeout. On timeout the title is simply skipped.
		ctx, cancel := context.WithTimeout(t.ctx, reasoningTitleTimeout)
		defer cancel()
		title, err := t.s.llm.GenerateReasoningTitle(llm.WithInferenceMetadata(ctx, inf), reasoning)
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
	t.stampTitles(trace)
}

// mergeIntoBlocks stamps collected titles onto the reasoning events inside every
// trace block. The blocks' trace events are separate objects from the flat
// trace, but share the same reasoning-event ids, so the same title map applies.
func (t *reasoningTitleTracker) mergeIntoBlocks(blocks []contentBlock) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range blocks {
		if blocks[i].Type != "trace" {
			continue
		}
		t.stampTitles(blocks[i].Events)
	}
}

// stampTitles writes each collected title onto its matching reasoning event.
// Callers must hold t.mu.
func (t *reasoningTitleTracker) stampTitles(events []activityTraceEvent) {
	for i := range events {
		if events[i].Type != "reasoning" {
			continue
		}
		if title, ok := t.titles[events[i].ID]; ok {
			events[i].Title = title
		}
	}
}

type reasoningTitleResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
