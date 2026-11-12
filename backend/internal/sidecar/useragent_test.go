package sidecar

import "testing"

func TestUserAgent(t *testing.T) {
	if got := UserAgent("loom-shop", "0.12.0"); got != "loom-shop/0.12.0 (+https://github.com/trick77/loom)" {
		t.Fatalf("UserAgent = %q", got)
	}
}
