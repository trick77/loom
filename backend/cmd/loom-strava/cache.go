package main

import (
	"container/list"
	"sync"
	"time"
)

// Cache sizes per user. Activity detail is effectively immutable, so a day is
// safe; lists change with every upload, so minutes. Streams are bounded far
// below the details: a 3 h ride at 1 Hz is ~10.8k samples x 11 series x 8 B
// (~1 MB parsed), so 300 of them would be ~300 MB per user. The metric store
// keeps what the baseline needs from them anyway.
const (
	detailCacheSize  = 300
	detailTTL        = 24 * time.Hour
	streamCacheSize  = 40
	listCacheSize    = 50
	listTTL          = 5 * time.Minute
	profileCacheSize = 100
	profileTTL       = time.Hour
)

// lru is a small size- and age-bounded cache, safe for concurrent use.
type lru struct {
	mu    sync.Mutex
	size  int
	ttl   time.Duration
	order *list.List // front = most recently used
	items map[string]*list.Element
}

type lruEntry struct {
	key string
	val any
	at  time.Time
}

func newLRU(size int, ttl time.Duration) *lru {
	return &lru{size: size, ttl: ttl, order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) get(key string, now time.Time) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*lruEntry)
	if now.Sub(e.at) >= c.ttl {
		c.order.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e.val, true
}

func (c *lru) put(key string, val any, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value = &lruEntry{key: key, val: val, at: now}
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&lruEntry{key: key, val: val, at: now})
	for c.order.Len() > c.size {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*lruEntry).key)
	}
}

func (c *lru) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// userCache is one user's cached Strava answers; never shared, dropped
// whole on connect and disconnect.
type userCache struct {
	detail  *lru // activity detail, activity zones
	streams *lru // parsed streams
	lists   *lru // activity list pages
	profile *lru // athlete, zones, stats, gear, routes, segments
}

func newUserCache() *userCache {
	return &userCache{
		detail:  newLRU(detailCacheSize, detailTTL),
		streams: newLRU(streamCacheSize, detailTTL),
		lists:   newLRU(listCacheSize, listTTL),
		profile: newLRU(profileCacheSize, profileTTL),
	}
}
