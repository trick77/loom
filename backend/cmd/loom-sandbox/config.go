package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultAddr = ":8070"
	// The model's code sees these fixed paths whatever the job directory is.
	sandboxInDir   = "/work/in"
	sandboxOutDir  = "/work/out"
	sandboxHomeDir = "/work/home"
	sandboxMain    = "/work/main.py"
	// workDir is a small tmpfs in the container; each job mounts its own sized
	// tmpfs below workDir/jobs.
	workDir = "/work"

	maxTimeout     = 60 * time.Second
	minTimeout     = time.Second
	maxCodeBytes   = 256 << 10
	maxInputFiles  = 10
	maxInputBytes  = 30 << 20
	maxRequestBody = 42 << 20 // base64 inflates the inputs by a third

	// stdout keeps its head (the answer is printed first), stderr its tail (the
	// traceback ends there). Together they fit loom's 32 KiB tool result cap.
	maxStdoutBytes = 24 << 10
	maxStderrBytes = 6 << 10

	maxOutputFiles = 10
	maxOutputBytes = 25 << 20

	slotUIDBase = 10000
)

type config struct {
	addr        string
	token       string
	slots       int
	insecureDev bool
	// Per job: address space of the interpreter and size of its private tmpfs
	// (inputs, outputs and home together).
	memLimit  uint64
	diskLimit uint64
	queueWait time.Duration
	// python is the interpreter; mplConfig the prebuilt matplotlib cache copied
	// into each job's home so a run does not rebuild the font list.
	python    string
	mplConfig string
}

func loadConfig(getenv func(string) string) (config, error) {
	or := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	cfg := config{
		addr:        or("SANDBOX_ADDR", defaultAddr),
		token:       getenv("SANDBOX_TOKEN"),
		insecureDev: getenv("SANDBOX_INSECURE_DEV") == "1",
		queueWait:   10 * time.Second,
		python:      or("SANDBOX_PYTHON", "/usr/local/bin/python3"),
		mplConfig:   or("SANDBOX_MPLCONFIG", "/opt/mplconfig"),
	}
	if len(cfg.token) < 16 {
		return config{}, errors.New("SANDBOX_TOKEN must be set to at least 16 characters")
	}
	var err error
	if cfg.slots, err = positiveInt(or("SANDBOX_SLOTS", "2")); err != nil {
		return config{}, fmt.Errorf("SANDBOX_SLOTS: %w", err)
	}
	mem, err := positiveInt(or("SANDBOX_MEM_LIMIT_MB", "1280"))
	if err != nil {
		return config{}, fmt.Errorf("SANDBOX_MEM_LIMIT_MB: %w", err)
	}
	disk, err := positiveInt(or("SANDBOX_DISK_LIMIT_MB", "320"))
	if err != nil {
		return config{}, fmt.Errorf("SANDBOX_DISK_LIMIT_MB: %w", err)
	}
	cfg.memLimit = uint64(mem) << 20
	cfg.diskLimit = uint64(disk) << 20
	return cfg, nil
}

func positiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("want a positive integer, got %q", s)
	}
	return n, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
