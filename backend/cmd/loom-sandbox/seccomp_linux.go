//go:build linux

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A job may start threads but no processes. RLIMIT_AS bounds one process, so
// only a single-process job has a fixed memory ceiling; with that, the slots'
// budgets add up to a known total (see checkMemoryBudget). Threads share
// their process's address space and stay allowed.
//
// The filter returns EAGAIN for clone without CLONE_THREAD and for fork and
// vfork, the same error a process limit gives, so Python raises a plain
// BlockingIOError. clone3 gets ENOSYS: its flags sit behind a pointer the
// filter cannot read, and libc then falls back to clone.

const (
	seccompRetKillProcess = 0x80000000
	seccompRetErrno       = 0x00050000
	seccompRetAllow       = 0x7fff0000

	seccompDataNr   = 0
	seccompDataArch = 4
	seccompDataArg0 = 16 // low 32 bits on little-endian
)

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}

// noProcessFilter builds the program. forkCalls are the architecture's plain
// fork-like system calls (fork and vfork on amd64; arm64 has none).
func noProcessFilter(arch uint32, forkCalls []uint32) []unix.SockFilter {
	const (
		ld   = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq  = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jset = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K
		ret  = unix.BPF_RET | unix.BPF_K
	)
	eagain := seccompRetErrno | uint32(unix.EAGAIN)
	prog := []unix.SockFilter{
		bpfStmt(ld, seccompDataArch),
		bpfJump(jeq, arch, 1, 0),
		bpfStmt(ret, seccompRetKillProcess),
		bpfStmt(ld, seccompDataNr),
		bpfJump(jeq, uint32(unix.SYS_CLONE3), 0, 1),
		bpfStmt(ret, seccompRetErrno|uint32(unix.ENOSYS)),
		bpfJump(jeq, uint32(unix.SYS_CLONE), 0, 4),
		bpfStmt(ld, seccompDataArg0),
		bpfJump(jset, unix.CLONE_THREAD, 0, 1),
		bpfStmt(ret, seccompRetAllow),
		bpfStmt(ret, eagain),
	}
	n := len(forkCalls)
	for j, nr := range forkCalls {
		prog = append(prog, bpfJump(jeq, nr, uint8(n-j), 0)) //nolint:gosec // n is a handful of syscalls
	}
	return append(prog, bpfStmt(ret, seccompRetAllow), bpfStmt(ret, eagain))
}

// installNoProcessFilter applies the filter to the calling thread, which then
// execs the interpreter. Requires no_new_privs.
func installNoProcessFilter() error {
	prog := noProcessFilter(auditArch, forkSyscalls)
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}                                                        //nolint:gosec // a fixed program of under 20 instructions
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&fprog)), 0, 0); err != nil { //nolint:gosec // prctl takes the filter by pointer
		return fmt.Errorf("seccomp: %w", err)
	}
	return nil
}
