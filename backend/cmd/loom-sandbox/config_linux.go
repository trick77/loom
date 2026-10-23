package main

import (
	"fmt"
	"path/filepath"
)

// The job limits the Linux executor applies; the sandbox runs on Linux only.
const (
	// workDir holds one tmpfs per slot (workDir/slot<N>, mounted by compose);
	// a job's working directory with in/, out/ and home/ lives on its slot's.
	// Separate mounts make the kernel enforce each slot's disk share.
	workDir = "/work"

	// stdout keeps its head (the answer is printed first), stderr its tail (the
	// traceback ends there). Together with the headers and file lines they stay
	// well under loom's 32 KiB tool result cap.
	maxStdoutBytes = 20 << 10
	maxStderrBytes = 4 << 10

	// slotUIDBase is far above any uid a host process uses: without a user
	// namespace, RLIMIT_NPROC counts the uid's tasks host-wide, and a job must
	// never share that count with something outside the sandbox.
	slotUIDBase = 3_000_000_000
	// jobThreadLimit caps one job's threads (RLIMIT_NPROC), well below the
	// container's pids_limit of 256 shared by both slots and the runner.
	jobThreadLimit = 64
)

// slotDir is the tmpfs a slot's jobs live on.
func slotDir(slot int) string {
	return filepath.Join(workDir, fmt.Sprintf("slot%d", slot))
}
