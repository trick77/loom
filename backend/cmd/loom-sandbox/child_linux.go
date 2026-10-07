//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const childCommand = "exec-child"

// execChild runs as PID 1 of a fresh PID, mount, network, IPC and UTS
// namespace set, still as root. It shapes the file system the job sees, drops
// to the slot uid, applies the resource limits and becomes the interpreter.
// It returns only on failure.
func execChild(args []string) error {
	if err := setupChild(args); err != nil {
		fmt.Fprintf(os.Stderr, "%s%v\n", childSetupMarker, err)
		os.Exit(childSetupExit)
	}
	return nil
}

func setupChild(args []string) error {
	// no_new_privs is per thread: setup and exec must stay on one OS thread,
	// or the interpreter may start on a thread that never got the flag.
	runtime.LockOSThread()
	if len(args) != 5 {
		return fmt.Errorf("want 5 arguments, got %d", len(args))
	}
	jobDir, python := args[0], args[4]
	uid, err := strconv.Atoi(args[1])
	if err != nil || uid < slotUIDBase {
		return fmt.Errorf("bad uid %q", args[1])
	}
	memLimit, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return fmt.Errorf("bad memory limit %q", args[2])
	}
	cpu, err := strconv.ParseUint(args[3], 10, 64)
	if err != nil {
		return fmt.Errorf("bad cpu limit %q", args[3])
	}
	if os.Getpid() != 1 {
		return fmt.Errorf("not PID 1 of a new PID namespace")
	}

	// Nothing mounted here may leak back into the server's namespace.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}
	// A /proc of the new PID namespace: the job sees only its own processes,
	// never the server and its environment.
	if err := syscall.Mount("proc", "/proc", "proc", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, ""); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}
	binds := []struct {
		src, dst string
		ro       bool
	}{
		{filepath.Join(jobDir, "in"), sandboxInDir, true},
		{filepath.Join(jobDir, "out"), sandboxOutDir, false},
		{filepath.Join(jobDir, "home"), sandboxHomeDir, false},
		{filepath.Join(jobDir, "main.py"), sandboxMain, true},
	}
	for _, b := range binds {
		if err := bindMount(b.src, b.dst, b.ro); err != nil {
			return err
		}
	}
	// Hide every job directory, this one included, behind an empty tmpfs: the
	// job reaches its files only through the fixed paths above.
	if err := syscall.Mount("tmpfs", filepath.Join(workDir, "jobs"), "tmpfs",
		syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC|syscall.MS_RDONLY, "size=4k,mode=0555"); err != nil {
		return fmt.Errorf("hide job directories: %w", err)
	}
	// Docker's /dev/shm (and /dev/mqueue) are shared, world-writable and
	// outlive a job; a CLONE_NEWIPC namespace does not cover them. Each job gets
	// its own, gone with its mount namespace.
	if err := syscall.Mount("tmpfs", "/dev/shm", "tmpfs",
		syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "size=64m,mode=1777"); err != nil {
		return fmt.Errorf("private /dev/shm: %w", err)
	}
	if _, err := os.Stat("/dev/mqueue"); err == nil {
		if err := syscall.Mount("tmpfs", "/dev/mqueue", "tmpfs",
			syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC|syscall.MS_RDONLY, "size=4k,mode=0555"); err != nil {
			return fmt.Errorf("hide /dev/mqueue: %w", err)
		}
	}
	_ = syscall.Sethostname([]byte("sandbox"))

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("setgroups: %w", err)
	}
	if err := syscall.Setresgid(uid, uid, uid); err != nil {
		return fmt.Errorf("setresgid: %w", err)
	}
	if err := syscall.Setresuid(uid, uid, uid); err != nil {
		return fmt.Errorf("setresuid: %w", err)
	}
	if err := os.Chdir(sandboxHomeDir); err != nil {
		return fmt.Errorf("chdir: %w", err)
	}

	// Limits last, right before exec: lowering them needs no privilege, and the
	// address-space cap must not starve this Go process first. Setting
	// RLIMIT_NOFILE explicitly also stops Go from restoring the inherited value
	// on exec.
	limits := []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_CORE, 0},
		{unix.RLIMIT_CPU, cpu},
		{unix.RLIMIT_FSIZE, 64 << 20},
		{unix.RLIMIT_NPROC, 64},
		{unix.RLIMIT_NOFILE, 256},
		{unix.RLIMIT_AS, memLimit},
	}
	for _, l := range limits {
		if err := unix.Setrlimit(l.resource, &unix.Rlimit{Cur: l.value, Max: l.value}); err != nil {
			return fmt.Errorf("setrlimit %d: %w", l.resource, err)
		}
	}

	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + sandboxHomeDir,
		"TMPDIR=" + sandboxHomeDir,
		"LANG=C.UTF-8",
		"MPLBACKEND=Agg",
		"MPLCONFIGDIR=" + sandboxHomeDir + "/.config/matplotlib",
		"XDG_CACHE_HOME=" + sandboxHomeDir + "/.cache",
		// BLAS and OpenMP otherwise start a thread per host CPU, each counting
		// against RLIMIT_NPROC and the address-space cap.
		"OPENBLAS_NUM_THREADS=1",
		"OMP_NUM_THREADS=1",
		"MKL_NUM_THREADS=1",
	}
	// -I: ignore PYTHON* variables and the user site; -B: no .pyc writes;
	// -u: unbuffered, so output printed before a kill still arrives.
	argv := []string{"python3", "-I", "-B", "-u", "-X", "utf8", sandboxMain}
	coverageFlush()
	return syscall.Exec(python, argv, env) //nolint:gosec // python is the operator-configured interpreter path; running untrusted code is this binary's purpose
}

func bindMount(src, dst string, ro bool) error {
	if err := syscall.Mount(src, dst, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind %s: %w", dst, err)
	}
	flags := uintptr(syscall.MS_BIND | syscall.MS_REMOUNT | syscall.MS_NOSUID | syscall.MS_NODEV)
	if ro {
		flags |= syscall.MS_RDONLY
	}
	if err := syscall.Mount("", dst, "", flags, ""); err != nil {
		return fmt.Errorf("remount %s: %w", dst, err)
	}
	return nil
}
