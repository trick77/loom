package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCollectOutputsKeepsOnlySafeRegularFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("chart.png", "png")
	write("result.csv", "a,b")
	write("evil.svg", "<svg onload=x>")
	write("script.py", "x")
	write("linked.txt", "shared")
	if err := os.Link(filepath.Join(dir, "linked.txt"), filepath.Join(dir, "linked2.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "passwd.txt")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.txt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, dropped, err := collectOutputs(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "chart.png,result.csv" {
		t.Fatalf("kept %v", names)
	}
	report := strings.Join(dropped, "\n")
	for _, want := range []string{"evil.svg", "script.py", "linked.txt", "linked2.txt", "passwd.txt", "pipe.txt", "sub.txt"} {
		if !strings.Contains(report, want) {
			t.Errorf("%s not reported as dropped:\n%s", want, report)
		}
	}
}

func TestCollectOutputsEnforcesLimits(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxOutputFiles+2; i++ {
		name := filepath.Join(dir, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, dropped, err := collectOutputs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != maxOutputFiles || len(dropped) != 2 {
		t.Fatalf("files %d dropped %v", len(files), dropped)
	}

	bigDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bigDir, "big.csv"), make([]byte, maxOutputBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	files, dropped, _ = collectOutputs(bigDir)
	if len(files) != 0 || len(dropped) != 1 || !strings.Contains(dropped[0], "size") {
		t.Fatalf("big file: %v %v", files, dropped)
	}
}

func TestCollectOutputsBoundsAFlood(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxOutputEntries+50; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.bin", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, dropped, err := collectOutputs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || len(dropped) != maxDroppedReported+2 {
		t.Fatalf("files %d, dropped %d: %v", len(files), len(dropped), dropped[len(dropped)-2:])
	}
	if !strings.Contains(dropped[maxDroppedReported], "more files not delivered") ||
		!strings.Contains(dropped[maxDroppedReported+1], "not examined") {
		t.Fatalf("summary lines: %v", dropped[maxDroppedReported:])
	}
}

func TestCollectOutputsMissingOrReplacedDir(t *testing.T) {
	files, dropped, err := collectOutputs(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(files) != 0 || len(dropped) != 1 {
		t.Fatalf("missing out/: %v %v %v", files, dropped, err)
	}

	// out/ swapped for a symlink to a directory with a readable file: never
	// followed, so nothing outside the job comes back.
	base := t.TempDir()
	secret := filepath.Join(base, "secret")
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "data.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "out")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	files, dropped, err = collectOutputs(link)
	if err != nil || len(files) != 0 || len(dropped) != 1 {
		t.Fatalf("symlinked out/: %v %v %v", files, dropped, err)
	}
}
