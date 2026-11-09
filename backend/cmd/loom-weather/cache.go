package main

import (
	"context"
	"sync"
	"time"
)

// cache keeps provider answers by key until their expiry and folds concurrent
// misses on one key into one fetch: every loom user in one place shares the
// one request. Errors are never kept: the next call asks again.
type cache[V any] struct {
	mu       sync.Mutex
	entries  map[string]cacheEntry[V]
	inflight map[string]*cacheCall[V]
	now      func() time.Time
	// limit bounds the entries; past it the expired go, then all.
	limit int
}

type cacheEntry[V any] struct {
	value   V
	expires time.Time
}

type cacheCall[V any] struct {
	done  chan struct{}
	value V
	err   error
}

func newCache[V any](now func() time.Time, limit int) *cache[V] {
	return &cache[V]{entries: map[string]cacheEntry[V]{}, inflight: map[string]*cacheCall[V]{}, now: now, limit: limit}
}

// get returns the kept value for key, else fetches it once for every caller
// waiting on key and keeps it until expires(now). The fetch runs detached
// from any one caller, so a caller that gives up does not fail the others.
func (c *cache[V]) get(ctx context.Context, key string, expires func(time.Time) time.Time, fetch func(context.Context) (V, error)) (V, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && c.now().Before(e.expires) {
		c.mu.Unlock()
		return e.value, nil
	}
	call, running := c.inflight[key]
	if !running {
		call = &cacheCall[V]{done: make(chan struct{})}
		c.inflight[key] = call
		go c.run(context.WithoutCancel(ctx), key, call, expires, fetch)
	}
	c.mu.Unlock()
	select {
	case <-call.done:
		return call.value, call.err
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

func (c *cache[V]) run(ctx context.Context, key string, call *cacheCall[V], expires func(time.Time) time.Time, fetch func(context.Context) (V, error)) {
	call.value, call.err = fetch(ctx)
	c.mu.Lock()
	delete(c.inflight, key)
	if call.err == nil {
		c.prune()
		now := c.now()
		c.entries[key] = cacheEntry[V]{value: call.value, expires: expires(now)}
	}
	c.mu.Unlock()
	close(call.done)
}

// prune makes room for one more entry; the caller holds mu.
func (c *cache[V]) prune() {
	if len(c.entries) < c.limit {
		return
	}
	now := c.now()
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= c.limit {
		clear(c.entries)
	}
}

// nextHour is the top of the hour after now: forecasts change with model runs
// every few hours, and loom reads "now" from the hourly values itself, so one
// fetch per place and hour shows everything there is.
func nextHour(now time.Time) time.Time {
	return now.Truncate(time.Hour).Add(time.Hour)
}

// geocodeTTL keeps a place-name lookup: names and their coordinates do not move.
const geocodeTTL = 7 * 24 * time.Hour

func afterGeocodeTTL(now time.Time) time.Time { return now.Add(geocodeTTL) }
