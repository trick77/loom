//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// childSetupExit is the exit code exec-child uses when it fails before the
// interpreter starts; together with childSetupMarker on stderr it tells an
// infrastructure failure apart from user code that exits 125.
const (
	childSetupExit   = 125
	childSetupMarker = "loom-sandbox: setup: "
)

type linuxExecutor struct {
	cfg config
}

func newExecutor(cfg config) (executor, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("serve must run as root inside the sandbox container")
	}
	// Mount points the child binds the job's directories onto. workDir is a
	// tmpfs (the root file system is read-only), so these are recreated on every
	// container start.
	for _, d := range []string{filepath.Join(workDir, "jobs"), sandboxInDir, sandboxOutDir, sandboxHomeDir} {
		if err := os.MkdirAll(d, 0o711); err != nil { //nolint:gosec // mount points the unprivileged slot uid must traverse
			return nil, err
		}
	}
	f, err := os.OpenFile(sandboxMain, os.O_CREATE|os.O_WRONLY, 0o444) //nolint:gosec // bind target for the job script, read by the slot uid
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &linuxExecutor{cfg: cfg}, nil
}

func (e *linuxExecutor) run(ctx context.Context, j job) (runResponse, error) {
	uid := slotUIDBase + j.slot
	jobDir, cleanup, err := e.prepare(j, uid)
	if err != nil {
		return runResponse{}, err
	}
	defer cleanup()

	stdout := &headBuffer{max: maxStdoutBytes}
	stderr := &tailBuffer{max: maxStderrBytes}
	cpu := int(j.timeout/time.Second) + 1
	cmd := exec.Command("/proc/self/exe", childCommand, //nolint:gosec // re-exec of this binary; the arguments are server-made
		jobDir, strconv.Itoa(uid), strconv.FormatUint(e.cfg.memLimit, 10), strconv.Itoa(cpu), e.cfg.python)
	cmd.Env = childEnv()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// The child is PID 1 of its own PID namespace: killing it kills every
		// process the job started. The empty network namespace has no route
		// anywhere, not even to this server.
		Cloneflags: syscall.CLONE_NEWNET | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS |
			syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS,
		// No Pdeathsig: Go compares getppid() with the parent's PID after the
		// clone, which differs inside a new PID namespace under gVisor, so the
		// child SIGKILLs itself on start. Not needed either: the server is PID
		// 1 of the container, its death ends every job.
	}
	if err := cmd.Start(); err != nil {
		return runResponse{}, fmt.Errorf("start child: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(j.timeout)
	defer timer.Stop()
	timedOut := false
	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		_ = cmd.Process.Kill()
		waitErr = <-done
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return runResponse{}, ctx.Err()
	}

	exitCode := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if !errors.As(waitErr, &ee) {
			return runResponse{}, fmt.Errorf("wait child: %w", waitErr)
		}
		exitCode = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			exitCode = 128 + int(ws.Signal())
		}
	}
	errText := stderr.String()
	if exitCode == childSetupExit && strings.Contains(errText, childSetupMarker) {
		return runResponse{}, fmt.Errorf("child setup failed: %s", strings.TrimSpace(errText))
	}

	files, dropped, err := collectOutputs(filepath.Join(jobDir, "out"))
	if err != nil {
		return runResponse{}, fmt.Errorf("collect outputs: %w", err)
	}
	return runResponse{
		Stdout:    stdout.String(),
		Stderr:    errText,
		ExitCode:  exitCode,
		TimedOut:  timedOut,
		Truncated: stdout.truncated || stderr.truncated,
		Files:     files,
		Dropped:   dropped,
	}, nil
}

// prepare builds the job directory on its own size-limited tmpfs: in/ (root
// owned, read-only inputs), out/ and home/ (owned by the slot uid), main.py.
// The returned cleanup unmounts the tmpfs, which frees everything at once.
func (e *linuxExecutor) prepare(j job, uid int) (string, func(), error) {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return "", nil, err
	}
	jobDir := filepath.Join(workDir, "jobs", hex.EncodeToString(id))
	if err := os.Mkdir(jobDir, 0o700); err != nil {
		return "", nil, err
	}
	opts := fmt.Sprintf("size=%d,mode=0711", e.cfg.diskLimit)
	if err := syscall.Mount("tmpfs", jobDir, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, opts); err != nil {
		_ = os.Remove(jobDir)
		return "", nil, fmt.Errorf("mount job tmpfs: %w", err)
	}
	cleanup := func() {
		_ = syscall.Unmount(jobDir, syscall.MNT_DETACH)
		_ = os.Remove(jobDir)
	}
	if err := populateJobDir(jobDir, j, uid, e.cfg.mplConfig); err != nil {
		cleanup()
		return "", nil, err
	}
	return jobDir, cleanup, nil
}

func populateJobDir(jobDir string, j job, uid int, mplConfig string) error {
	in := filepath.Join(jobDir, "in")
	if err := os.Mkdir(in, 0o755); err != nil { //nolint:gosec // inputs are read by the slot uid, written only here
		return err
	}
	for _, f := range j.inputs {
		if err := os.WriteFile(filepath.Join(in, f.Name), f.Data, 0o444); err != nil { //nolint:gosec // read-only input for the slot uid; the name passed validName
			return err
		}
	}
	for _, d := range []string{"out", "home"} {
		p := filepath.Join(jobDir, d)
		if err := os.Mkdir(p, 0o700); err != nil {
			return err
		}
		if err := os.Chown(p, uid, uid); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(jobDir, "main.py"), []byte(j.code), 0o444); err != nil { //nolint:gosec // the job script, read by the slot uid
		return err
	}
	if mplConfig != "" {
		if err := copyTree(mplConfig, filepath.Join(jobDir, "home", ".config", "matplotlib"), uid); err != nil {
			return fmt.Errorf("copy matplotlib cache: %w", err)
		}
	}
	return nil
}

// copyTree copies a small, trusted directory (the image's matplotlib cache)
// and hands it to uid. A missing source is not an error: matplotlib then
// rebuilds its cache, only slower.
func copyTree(src, dst string, uid int) error {
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) { //nolint:gosec // src is the image's matplotlib cache from config, not job input
		return nil
	}
	for _, d := range []string{filepath.Dir(dst), dst} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := os.Chown(d, uid, uid); err != nil {
			return err
		}
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error { //nolint:gosec // see above: a trusted image directory
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.Mkdir(target, 0o700); err != nil {
				return err
			}
			return os.Chown(target, uid, uid)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(path) //nolint:gosec // inside the trusted image directory
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // target is under the fresh job home
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chown(target, uid, uid)
	})
}
