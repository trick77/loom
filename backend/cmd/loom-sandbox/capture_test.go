package main

import (
	"strings"
	"testing"
)

func TestHeadBufferKeepsTheStart(t *testing.T) {
	h := &headBuffer{max: 10}
	for _, s := range []string{"hello ", "world ", "and more"} {
		if n, err := h.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("write must accept everything: %d %v", n, err)
		}
	}
	got := h.String()
	if !strings.HasPrefix(got, "hello worl") || !strings.Contains(got, "truncated") || !h.truncated {
		t.Fatalf("got %q", got)
	}

	small := &headBuffer{max: 10}
	_, _ = small.Write([]byte("ok"))
	if small.String() != "ok" || small.truncated {
		t.Fatalf("small: %q", small.String())
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	tb := &tailBuffer{max: 8}
	_, _ = tb.Write([]byte("Traceback\n"))
	_, _ = tb.Write([]byte("KeyError"))
	got := tb.String()
	if !strings.HasSuffix(got, "KeyError") || !strings.HasPrefix(got, "[earlier output truncated]") {
		t.Fatalf("got %q", got)
	}

	small := &tailBuffer{max: 8}
	_, _ = small.Write([]byte("err"))
	if small.String() != "err" {
		t.Fatalf("small: %q", small.String())
	}
}

func TestBuffersDoNotSplitRunes(t *testing.T) {
	h := &headBuffer{max: 5}
	_, _ = h.Write([]byte("abcdé")) // é is two bytes: the cut lands inside it
	if got := h.String(); !strings.HasPrefix(got, "abcd\n") {
		t.Fatalf("head: %q", got)
	}
	tb := &tailBuffer{max: 3}
	_, _ = tb.Write([]byte("xéyz")) // keeps the second byte of é, then "yz"
	if got := tb.String(); !strings.HasSuffix(got, "\nyz") {
		t.Fatalf("tail: %q", got)
	}
}
