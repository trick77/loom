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
	// workDir is the container's tmpfs for job directories (workDir/jobs/<id>,
	// the job's working directory with in/, out/ and home/). compose sizes it
	// to slots × SANDBOX_DISK_LIMIT_MB.
	workDir = "/work"

	maxTimeout     = 60 * time.Second
	minTimeout     = time.Second
	maxCodeBytes   = 256 << 10
	maxInputFiles  = 10
	maxInputBytes  = 30 << 20
	maxRequestBody = 42 << 20 // base64 inflates the inputs by a third

	// stdout keeps its head (the answer is printed first), stderr its tail (the
	// traceback ends there). Together with the headers and file lines they stay
	// well under loom's 32 KiB tool result cap.
	maxStdoutBytes = 20 << 10
	maxStderrBytes = 4 << 10

	maxOutputFiles = 10
	maxOutputBytes = 25 << 20
	// Bounds on what a job can make the server scan and report from out/.
	maxOutputEntries   = 200
	maxDroppedReported = 20

	slotUIDBase = 10000

	// sharedShmBytes is the container's /dev/shm (compose shm_size), shared by
	// the slots and emptied of a job's files after it.
	sharedShmBytes = 64 << 20
)

type config struct {
	addr  string
	token string // optional; empty accepts any request on the internal network
	slots int
	// Per job: address space of the interpreter, and its share of the /work
	// tmpfs (inputs, outputs and home together).
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
		addr:      or("SANDBOX_ADDR", defaultAddr),
		token:     getenv("SANDBOX_TOKEN"),
		queueWait: 10 * time.Second,
		python:    or("SANDBOX_PYTHON", "/usr/local/bin/python3"),
		mplConfig: or("SANDBOX_MPLCONFIG", "/opt/mplconfig"),
	}
	if cfg.token != "" && len(cfg.token) < 16 {
		return config{}, errors.New("SANDBOX_TOKEN, when set, must be at least 16 characters")
	}
	var err error
	if cfg.slots, err = positiveInt(or("SANDBOX_SLOTS", "2")); err != nil {
		return config{}, fmt.Errorf("SANDBOX_SLOTS: %w", err)
	}
	if cfg.memLimit, err = megabytes(or("SANDBOX_MEM_LIMIT_MB", "1280")); err != nil {
		return config{}, fmt.Errorf("SANDBOX_MEM_LIMIT_MB: %w", err)
	}
	if cfg.diskLimit, err = megabytes(or("SANDBOX_DISK_LIMIT_MB", "320")); err != nil {
		return config{}, fmt.Errorf("SANDBOX_DISK_LIMIT_MB: %w", err)
	}
	total, err := megabytes(or("SANDBOX_TOTAL_MEMORY_MB", "3584"))
	if err != nil {
		return config{}, fmt.Errorf("SANDBOX_TOTAL_MEMORY_MB: %w", err)
	}
	// Every job is one process with no anonymous files (see seccomp_linux.go),
	// so its worst case is fixed: its address space and its share of the
	// /work tmpfs. All slots full plus the shared /dev/shm must fit the total,
	// which leaves the container's mem_limit room for the server; a job that
	// wants more fails alone with MemoryError.
	if need := uint64(cfg.slots)*cfg.jobMemoryCeiling() + sharedShmBytes; need > total { //nolint:gosec // slots passed positiveInt
		return config{}, fmt.Errorf("%d slots × %d MiB per job + %d MiB /dev/shm = %d MiB exceeds SANDBOX_TOTAL_MEMORY_MB (%d MiB)",
			cfg.slots, cfg.jobMemoryCeiling()>>20, sharedShmBytes>>20, need>>20, total>>20)
	}
	return cfg, nil
}

// jobMemoryCeiling is the most memory one job can hold at once.
func (c config) jobMemoryCeiling() uint64 {
	return c.memLimit + c.diskLimit
}

func megabytes(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("want a positive number of megabytes, got %q", s)
	}
	return n << 20, nil
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
