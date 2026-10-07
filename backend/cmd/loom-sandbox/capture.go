package main

import (
	"sync"
	"unicode/utf8"
)

// headBuffer keeps the first max bytes written and discards the rest, while
// always accepting the write so the child never blocks on a full pipe.
type headBuffer struct {
	mu        sync.Mutex
	max       int
	buf       []byte
	truncated bool
}

func (h *headBuffer) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room := h.max - len(h.buf); room > 0 {
		if len(p) <= room {
			h.buf = append(h.buf, p...)
			return len(p), nil
		}
		h.buf = append(h.buf, p[:room]...)
	}
	if len(p) > 0 {
		h.truncated = true
	}
	return len(p), nil
}

func (h *headBuffer) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := trimInvalidUTF8(h.buf, false)
	if h.truncated {
		s += "\n…[output truncated]"
	}
	return s
}

// tailBuffer keeps the last max bytes written.
type tailBuffer struct {
	mu        sync.Mutex
	max       int
	buf       []byte
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.truncated = true
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := trimInvalidUTF8(t.buf, true)
	if t.truncated {
		s = "[earlier output truncated]…\n" + s
	}
	return s
}

// trimInvalidUTF8 drops a rune cut in half at the cut edge (the end for a
// head, the start for a tail) and replaces any other invalid bytes.
func trimInvalidUTF8(b []byte, cutAtStart bool) string {
	if cutAtStart {
		for i := 0; i < len(b) && i < utf8.UTFMax; i++ {
			if utf8.RuneStart(b[i]) {
				b = b[i:]
				break
			}
		}
	} else {
		for i := 0; i < utf8.UTFMax && len(b) > 0; i++ {
			r, size := utf8.DecodeLastRune(b)
			if r != utf8.RuneError || size != 1 {
				break
			}
			b = b[:len(b)-1]
		}
	}
	return string([]rune(string(b)))
}
