package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root-owned host dir (Docker creates a missing bind mount so) fails the
// start with the fix in the message; a writable one leaves nothing behind.
func TestWritable(t *testing.T) {
	dir := t.TempDir()
	if err := writable(dir); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("the check left %v", left)
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := writable(ro); err == nil || !strings.Contains(err.Error(), "chown 65532:65532") {
		t.Errorf("read-only dir: %v", err)
	}
}
