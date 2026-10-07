//go:build linux && amd64

package main

import "golang.org/x/sys/unix"

const auditArch = unix.AUDIT_ARCH_X86_64

var forkSyscalls = []uint32{unix.SYS_FORK, unix.SYS_VFORK}
