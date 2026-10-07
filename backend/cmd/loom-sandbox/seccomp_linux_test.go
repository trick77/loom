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
			if acc == in.K {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K:
			if acc&in.K != 0 {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case unix.BPF_RET | unix.BPF_K:
			return in.K
		default:
			t.Fatalf("pc %d: unexpected opcode %#x", pc, in.Code)
		}
	}
	t.Fatal("program fell off the end")
	return 0
}

func TestNoProcessFilter(t *testing.T) {
	const arch, fork, vfork, other = 0xc000003e, 57, 58, 1
	prog := noProcessFilter(arch, []uint32{fork, vfork})
	eagain := seccompRetErrno | uint32(unix.EAGAIN)
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
		{"anything else", arch, other, 0, seccompRetAllow},
		{"foreign arch", 0x40000003, other, 0, seccompRetKillProcess},
	}
	for _, c := range cases {
		if got := eval(t, prog, c.arch, c.nr, c.arg); got != c.want {
			t.Errorf("%s: got %#x, want %#x", c.name, got, c.want)
		}
	}
	// Without fork calls (arm64) the program still ends in allow/deny.
	if got := eval(t, noProcessFilter(arch, nil), arch, other, 0); got != seccompRetAllow {
		t.Errorf("no fork calls: got %#x", got)
	}
}
