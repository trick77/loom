//go:build linux

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The job filter is installed right before exec, on top of Docker's default
// seccomp profile. It enforces what the namespaces of a privileged sandbox
// would otherwise give, without any capability:
//
//   - one process: threads only (clone with CLONE_THREAD); fork, vfork and
//     process clones get EAGAIN, the error a process limit gives, so Python
//     raises a plain BlockingIOError. clone3 gets ENOSYS: its flags sit behind
//     a pointer the filter cannot read, and libc then falls back to clone.
//     RLIMIT_AS bounds one process, so only this keeps a job's memory fixed.
//   - no network: socket() only for AF_UNIX; io_uring, which can open
//     sockets behind the filter's back, is refused.
//   - no unbounded memory outside RLIMIT_AS: memfd_create (anonymous files),
//     System V IPC, POSIX message queues and new namespaces (a private tmpfs)
//     are refused. What remains are files on the slot's tmpfs and on
//     /dev/shm (POSIX shared memory), both sized by compose, counted in the
//     memory budget and emptied after the job.
//   - nothing that reaches other processes or kernel surfaces a job never
//     needs.

const (
	seccompRetKillProcess = 0x80000000
	seccompRetErrno       = 0x00050000
	seccompRetAllow       = 0x7fff0000

	seccompDataNr   = 0
	seccompDataArch = 4
	seccompDataArg0 = 16 // low 32 bits on little-endian

	// x32Bit marks x32-ABI system call numbers on amd64; they would slip past
	// rules written for the native numbers.
	x32Bit = 0x40000000
)

// deniedSyscalls get EPERM. Docker's default profile refuses some of them
// already; listing them here keeps the job's boundary independent of it.
var deniedSyscalls = []uint32{
	unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT,
	unix.SYS_MEMFD_CREATE,
	unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER,
	unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
	unix.SYS_USERFAULTFD, unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN,
	unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY,
	unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_NAME_TO_HANDLE_AT,
	// System V and POSIX IPC objects hold memory outside RLIMIT_AS and outlive
	// the job (the next job on the same uid could read them).
	unix.SYS_SHMGET, unix.SYS_SHMAT, unix.SYS_SHMCTL, unix.SYS_SHMDT,
	unix.SYS_MSGGET, unix.SYS_MSGSND, unix.SYS_MSGRCV, unix.SYS_MSGCTL,
	unix.SYS_SEMGET, unix.SYS_SEMOP, unix.SYS_SEMCTL, unix.SYS_SEMTIMEDOP,
	unix.SYS_MQ_OPEN, unix.SYS_MQ_UNLINK, unix.SYS_MQ_TIMEDSEND, unix.SYS_MQ_TIMEDRECEIVE,
	unix.SYS_MQ_NOTIFY, unix.SYS_MQ_GETSETATTR,
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}

// jobFilter builds the program for one architecture. forkCalls are its plain
// fork-like system calls (fork and vfork on amd64; arm64 has none); x32 says
// whether x32-ABI numbers exist and must be killed.
func jobFilter(arch uint32, x32 bool, forkCalls []uint32) []unix.SockFilter {
	const (
		ld   = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq  = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jge  = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		jset = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K
		ret  = unix.BPF_RET | unix.BPF_K
	)
	eagain := seccompRetErrno | uint32(unix.EAGAIN)
	eperm := seccompRetErrno | uint32(unix.EPERM)
	prog := []unix.SockFilter{
		bpfStmt(ld, seccompDataArch),
		bpfJump(jeq, arch, 1, 0),
		bpfStmt(ret, seccompRetKillProcess),
		bpfStmt(ld, seccompDataNr),
	}
	if x32 {
		prog = append(prog, bpfJump(jge, x32Bit, 0, 1), bpfStmt(ret, seccompRetKillProcess))
	}
	deny := func(nr, action uint32) {
		prog = append(prog, bpfJump(jeq, nr, 0, 1), bpfStmt(ret, action))
	}
	deny(uint32(unix.SYS_CLONE3), seccompRetErrno|uint32(unix.ENOSYS))
	for _, nr := range forkCalls {
		deny(nr, eagain)
	}
	for _, nr := range deniedSyscalls {
		deny(nr, eperm)
	}
	// Each block below returns on every path, so a call that is not its
	// syscall skips it with the number still loaded.
	prog = append(prog,
		bpfJump(jeq, uint32(unix.SYS_CLONE), 0, 4),
		bpfStmt(ld, seccompDataArg0),
		bpfJump(jset, unix.CLONE_THREAD, 0, 1),
		bpfStmt(ret, seccompRetAllow),
		bpfStmt(ret, eagain),

		bpfJump(jeq, uint32(unix.SYS_SOCKET), 0, 4),
		bpfStmt(ld, seccompDataArg0),
		bpfJump(jeq, unix.AF_UNIX, 0, 1),
		bpfStmt(ret, seccompRetAllow),
		bpfStmt(ret, eperm),

		bpfStmt(ret, seccompRetAllow),
	)
	return prog
}

// installJobFilter applies the filter to the calling thread, which then
// execs the interpreter. Requires no_new_privs.
func installJobFilter() error {
	prog := jobFilter(auditArch, hasX32, forkSyscalls)
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}                                                        //nolint:gosec // a fixed program of a few dozen instructions
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&fprog)), 0, 0); err != nil { //nolint:gosec // prctl takes the filter by pointer
		return fmt.Errorf("seccomp: %w", err)
	}
	return nil
}
