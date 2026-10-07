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

// execChild runs as root in the job's directory. It drops to the slot uid,
// applies the resource limits and the seccomp filter, and becomes the
// interpreter. It returns only on failure.
func execChild(args []string) error {
	if err := setupChild(args); err != nil {
		// The private setup pipe, not stderr: the server must tell a sandbox
		// failure apart from anything the job's own code could print.
		_, _ = fmt.Fprintf(os.NewFile(setupErrFD, "setup"), "%v\n", err)
		os.Exit(1)
	}
	return nil
}

func setupChild(args []string) error {
	// no_new_privs and the seccomp filter are per thread: setup and exec must
	// stay on one OS thread, or the interpreter may start on a thread that
	// never got them.
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
	// Everything the job creates (here, in /dev/shm, anywhere writable) is
	// private to its uid, so the other slot cannot read it.
	syscall.Umask(0o077)
	if err := os.Chdir(jobDir); err != nil {
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

	home := filepath.Join(jobDir, "home")
	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + home,
		"TMPDIR=" + home,
		"LANG=C.UTF-8",
		"MPLBACKEND=Agg",
		"MPLCONFIGDIR=" + home + "/.config/matplotlib",
		"XDG_CACHE_HOME=" + home + "/.cache",
		// BLAS and OpenMP otherwise start a thread per host CPU, each counting
		// against RLIMIT_NPROC.
		"OPENBLAS_NUM_THREADS=1",
		"OMP_NUM_THREADS=1",
		"MKL_NUM_THREADS=1",
	}
	// -I: ignore PYTHON* variables and the user site; -B: no .pyc writes;
	// -u: unbuffered, so output printed before a kill still arrives.
	argv := []string{"python3", "-I", "-B", "-u", "-X", "utf8", "main.py"}
	coverageFlush()
	if err := installJobFilter(); err != nil {
		return err
	}
	// The setup pipe closes on exec: the interpreter never holds it.
	unix.CloseOnExec(setupErrFD)
	return syscall.Exec(python, argv, env) //nolint:gosec // python is the operator-configured interpreter path; running untrusted code is this binary's purpose
}
