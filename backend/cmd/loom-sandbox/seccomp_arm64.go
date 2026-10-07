//go:build linux && arm64

package main

import "golang.org/x/sys/unix"

const auditArch = unix.AUDIT_ARCH_AARCH64

// arm64 has no fork or vfork system call; everything goes through clone.
var forkSyscalls []uint32
