package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// runRequest is the wire format loom posts to /run.
type runRequest struct {
	Code      string     `json:"code"`
	Files     []wireFile `json:"files,omitempty"`
	TimeoutMS int64      `json:"timeout_ms,omitempty"`
}

type wireFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"` // base64 in JSON
}

type runResponse struct {
	Stdout     string     `json:"stdout"`
	Stderr     string     `json:"stderr"`
	ExitCode   int        `json:"exit_code"`
	TimedOut   bool       `json:"timed_out"`
	Truncated  bool       `json:"truncated"`
	Files      []wireFile `json:"files"`
	Dropped    []string   `json:"dropped,omitempty"`
	DurationMS int64      `json:"duration_ms"`
}

// job is a validated request bound to a slot.
type job struct {
	code    string
	inputs  []wireFile
	timeout time.Duration
	slot    int
}

// executor runs one job. The Linux implementation spawns the namespaced child;
// tests substitute a fake.
type executor interface {
	run(ctx context.Context, j job) (runResponse, error)
}

// safeName is the only filename shape that crosses the boundary in either
// direction: loom maps real upload names to such aliases, and output files with
// any other name are dropped.
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

var allowedOutputExt = map[string]bool{
	".png": true, ".csv": true, ".xlsx": true, ".json": true, ".txt": true, ".md": true,
}

func validName(name string) bool {
	return safeName.MatchString(name) && !strings.Contains(name, "..")
}

func validateRequest(req runRequest) (job, error) {
	if strings.TrimSpace(req.Code) == "" {
		return job{}, errors.New("code is empty")
	}
	if len(req.Code) > maxCodeBytes {
		return job{}, fmt.Errorf("code exceeds %d bytes", maxCodeBytes)
	}
	if len(req.Files) > maxInputFiles {
		return job{}, fmt.Errorf("at most %d input files", maxInputFiles)
	}
	seen := map[string]bool{}
	total := 0
	for _, f := range req.Files {
		if !validName(f.Name) {
			return job{}, fmt.Errorf("invalid input file name %q", f.Name)
		}
		if seen[f.Name] {
			return job{}, fmt.Errorf("duplicate input file name %q", f.Name)
		}
		seen[f.Name] = true
		total += len(f.Data)
	}
	if total > maxInputBytes {
		return job{}, fmt.Errorf("input files exceed %d bytes", maxInputBytes)
	}
	return job{code: req.Code, inputs: req.Files, timeout: clampTimeout(req.TimeoutMS)}, nil
}

func clampTimeout(ms int64) time.Duration {
	if ms <= 0 {
		return maxTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d < minTimeout {
		return minTimeout
	}
	if d > maxTimeout {
		return maxTimeout
	}
	return d
}
