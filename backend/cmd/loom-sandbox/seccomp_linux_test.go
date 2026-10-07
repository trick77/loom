//go:build linux

package main

import (
	"testing"

	"golang.org/x/sys/unix"
)

// eval runs the classic-BPF subset the filter uses against one system call.
func eval(t *testing.T, prog []unix.SockFilter, arch, nr, arg0 uint32) uint32 {
	t.Helper()
	var acc uint32
	for pc := 0; pc < len(prog); pc++ {
		in := prog[pc]
		jump := func(cond bool) {
			if cond {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		}
		switch in.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			switch in.K {
			case seccompDataArch:
				acc = arch
			case seccompDataNr:
				acc = nr
			case seccompDataArg0:
				acc = arg0
			default:
				t.Fatalf("pc %d: unexpected load offset %d", pc, in.K)
			}
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K:
			jump(acc == in.K)
		case unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K:
			jump(acc >= in.K)
		case unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K:
			jump(acc&in.K != 0)
		case unix.BPF_RET | unix.BPF_K:
			return in.K
		default:
			t.Fatalf("pc %d: unexpected opcode %#x", pc, in.Code)
		}
	}
	t.Fatal("program fell off the end")
	return 0
}

func TestJobFilter(t *testing.T) {
	const arch, fork, vfork, read = 0xc000003e, 57, 58, 0
	prog := jobFilter(arch, true, []uint32{fork, vfork})
	eagain := seccompRetErrno | uint32(unix.EAGAIN)
	eperm := seccompRetErrno | uint32(unix.EPERM)
	cases := []struct {
		name          string
		arch, nr, arg uint32
		want          uint32
	}{
		{"thread", arch, uint32(unix.SYS_CLONE), unix.CLONE_THREAD | unix.CLONE_VM, seccompRetAllow},
		{"process clone", arch, uint32(unix.SYS_CLONE), unix.CLONE_VM | unix.CLONE_VFORK, eagain},
		{"clone3", arch, uint32(unix.SYS_CLONE3), 0, seccompRetErrno | uint32(unix.ENOSYS)},
		{"fork", arch, fork, 0, eagain},
		{"vfork", arch, vfork, 0, eagain},
		{"unix socket", arch, uint32(unix.SYS_SOCKET), unix.AF_UNIX, seccompRetAllow},
		{"inet socket", arch, uint32(unix.SYS_SOCKET), unix.AF_INET, eperm},
		{"inet6 socket", arch, uint32(unix.SYS_SOCKET), unix.AF_INET6, eperm},
		{"netlink socket", arch, uint32(unix.SYS_SOCKET), unix.AF_NETLINK, eperm},
		{"memfd", arch, uint32(unix.SYS_MEMFD_CREATE), 0, eperm},
		{"unshare", arch, uint32(unix.SYS_UNSHARE), unix.CLONE_NEWUSER, eperm},
		{"io_uring", arch, uint32(unix.SYS_IO_URING_SETUP), 0, eperm},
		{"ptrace", arch, uint32(unix.SYS_PTRACE), 0, eperm},
		{"sysv shm", arch, uint32(unix.SYS_SHMGET), 0, eperm},
		{"sysv msg", arch, uint32(unix.SYS_MSGGET), 0, eperm},
		{"posix mq", arch, uint32(unix.SYS_MQ_OPEN), 0, eperm},
		{"x32 fork", arch, x32Bit | fork, 0, seccompRetKillProcess},
		{"anything else", arch, read, 0, seccompRetAllow},
		{"foreign arch", 0x40000003, read, 0, seccompRetKillProcess},
	}
	for _, c := range cases {
		if got := eval(t, prog, c.arch, c.nr, c.arg); got != c.want {
			t.Errorf("%s: got %#x, want %#x", c.name, got, c.want)
		}
	}
	// arm64: no fork calls, no x32 range.
	arm := jobFilter(arch, false, nil)
	if got := eval(t, arm, arch, x32Bit|read, 0); got != seccompRetAllow {
		t.Errorf("no x32 check: got %#x", got)
	}
	if got := eval(t, arm, arch, uint32(unix.SYS_CLONE), 0); got != eagain {
		t.Errorf("arm64 process clone: got %#x", got)
	}
}
