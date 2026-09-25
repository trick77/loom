package httpapi

import "sync"

// inflightKeys is a single-flight guard for the background refreshes: at most
// one memory (or description) regeneration per scope runs at a time. The
// refresh gates are check-then-act on stored counters, so without this every
// concurrent turn that read the stale count would start its own model call.
// A caller that loses simply returns; the next due check sees the fresh
// counters. The zero value is ready to use.
type inflightKeys struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

// tryAcquire claims key. ok is false while another caller holds it; otherwise
// release must be called once the work is done.
func (k *inflightKeys) tryAcquire(key string) (release func(), ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.keys == nil {
		k.keys = make(map[string]struct{})
	}
	if _, held := k.keys[key]; held {
		return nil, false
	}
	k.keys[key] = struct{}{}
	return func() {
		k.mu.Lock()
		delete(k.keys, key)
		k.mu.Unlock()
	}, true
}
