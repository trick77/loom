package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	defaultAddr = ":8070"
	// workDir holds one tmpfs per slot (workDir/slot<N>, mounted by compose);
	// a job's working directory with in/, out/ and home/ lives on its slot's.
	// Separate mounts make the kernel enforce each slot's disk share.
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

	// slotUIDBase is far above any uid a host process uses: without a user
	// namespace, RLIMIT_NPROC counts the uid's tasks host-wide, and a job must
	// never share that count with something outside the sandbox.
	slotUIDBase = 3_000_000_000
	// jobThreadLimit caps one job's threads (RLIMIT_NPROC), well below the
	// container's pids_limit of 256 shared by both slots and the runner.
	jobThreadLimit = 64

	// sharedShmBytes is the container's /dev/shm (compose shm_size), shared by
	// the slots and emptied of a job's files after it.
	sharedShmBytes = 64 << 20
)

type config struct {
	addr  string
	token string // optional; empty accepts any request on the internal network
	slots int
	// memLimit is the address space of one job's interpreter; totalMemory the
	// budget all slots must fit (see checkMemoryBudget).
	memLimit    uint64
	totalMemory uint64
	queueWait   time.Duration
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
	if cfg.totalMemory, err = megabytes(or("SANDBOX_TOTAL_MEMORY_MB", "3584")); err != nil {
		return config{}, fmt.Errorf("SANDBOX_TOTAL_MEMORY_MB: %w", err)
	}
	return cfg, nil
}

// slotDir is the tmpfs a slot's jobs live on.
func slotDir(slot int) string {
	return filepath.Join(workDir, fmt.Sprintf("slot%d", slot))
}

// checkMemoryBudget fails unless every slot full fits the total. A job is
// one process with no anonymous files (see seccomp_linux.go), so its worst
// case is fixed: its address space plus its slot's tmpfs, whose sizes come
// from the mounts themselves. With the shared /dev/shm on top, the total
// leaves the container's mem_limit room for the server; a job that wants
// more fails alone with MemoryError.
func checkMemoryBudget(cfg config, slotDisk []uint64) error {
	need := uint64(sharedShmBytes)
	for _, disk := range slotDisk {
		need += cfg.memLimit + disk
	}
	if need > cfg.totalMemory {
		return fmt.Errorf("%d slots (%d MiB address space each, slot tmpfs %v bytes) + %d MiB /dev/shm = %d MiB exceeds SANDBOX_TOTAL_MEMORY_MB (%d MiB)",
			len(slotDisk), cfg.memLimit>>20, slotDisk, sharedShmBytes>>20, need>>20, cfg.totalMemory>>20)
	}
	return nil
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
