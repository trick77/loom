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

// setupErrFD is the descriptor exec-child reports a setup failure on. It is
// closed on exec, so the job's code can never write to it: an error there is
// always the sandbox's, never the program's.
const setupErrFD = 3

type linuxExecutor struct {
	cfg config
}

func newExecutor(cfg config) (executor, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("serve must run as root inside the sandbox container")
	}
	// Traversable but not listable: a job finds its own directory, never the
	// other slot's.
	if err := os.MkdirAll(filepath.Join(workDir, "jobs"), 0o711); err != nil { //nolint:gosec // see above
		return nil, err
	}
	return &linuxExecutor{cfg: cfg}, nil
}

func (e *linuxExecutor) run(ctx context.Context, j job) (runResponse, error) {
	uid := slotUIDBase + j.slot
	jobDir, err := prepareJobDir(j, uid, e.cfg.mplConfig)
	if err != nil {
		return runResponse{}, err
	}
	defer cleanupJob(jobDir, uid)

	setupR, setupW, err := os.Pipe()
	if err != nil {
		return runResponse{}, err
	}
	defer func() { _ = setupR.Close() }()

	stdout := &headBuffer{max: maxStdoutBytes}
	stderr := &tailBuffer{max: maxStderrBytes}
	cpu := int(j.timeout/time.Second) + 1
	cmd := exec.Command("/proc/self/exe", childCommand, //nolint:gosec // re-exec of this binary; the arguments are server-made
		jobDir, strconv.Itoa(uid), strconv.FormatUint(e.cfg.memLimit, 10), strconv.Itoa(cpu), e.cfg.python)
	cmd.Env = childEnv()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.ExtraFiles = []*os.File{setupW} // fd 3 in the child
	cmd.WaitDelay = 2 * time.Second
	// The job is a single process (the seccomp filter refuses forks), so
	// killing it ends everything it started; its own group makes sure.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = setupW.Close()
		return runResponse{}, fmt.Errorf("start child: %w", err)
	}
	_ = setupW.Close()
	setupMsg := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(setupR, 4<<10))
		setupMsg <- strings.TrimSpace(string(b))
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	kill := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

	timer := time.NewTimer(j.timeout)
	defer timer.Stop()
	timedOut := false
	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		kill()
		waitErr = <-done
	case <-ctx.Done():
		kill()
		<-done
		return runResponse{}, ctx.Err()
	}
	if msg := <-setupMsg; msg != "" {
		return runResponse{}, fmt.Errorf("child setup failed: %s", msg)
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

	files, dropped, err := collectOutputs(filepath.Join(jobDir, "out"))
	if err != nil {
		return runResponse{}, fmt.Errorf("collect outputs: %w", err)
	}
	return runResponse{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  exitCode,
		TimedOut:  timedOut,
		Truncated: stdout.truncated || stderr.truncated,
		Files:     files,
		Dropped:   dropped,
	}, nil
}

// prepareJobDir builds /work/jobs/<random>, owned by the slot uid and closed
// to everyone else: in/ (read-only inputs), out/, home/ and main.py. The job
// runs with it as its working directory.
func prepareJobDir(j job, uid int, mplConfig string) (string, error) {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	jobDir := filepath.Join(workDir, "jobs", hex.EncodeToString(id))
	if err := os.Mkdir(jobDir, 0o700); err != nil {
		return "", err
	}
	if err := populateJobDir(jobDir, j, uid, mplConfig); err != nil {
		_ = os.RemoveAll(jobDir)
		return "", err
	}
	if err := os.Chown(jobDir, uid, uid); err != nil {
		_ = os.RemoveAll(jobDir)
		return "", err
	}
	return jobDir, nil
}

func populateJobDir(jobDir string, j job, uid int, mplConfig string) error {
	in := filepath.Join(jobDir, "in")
	if err := os.Mkdir(in, 0o755); err != nil { //nolint:gosec // inside the job's 0700 directory; read by the slot uid, written only here
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

// cleanupJob removes the job's directory and whatever it left in /dev/shm,
// the one other place it can write. Nothing of a job outlives it.
func cleanupJob(jobDir string, uid int) {
	_ = os.RemoveAll(jobDir) //nolint:gosec // jobDir is workDir/jobs/<random hex> built by prepareJobDir
	entries, err := os.ReadDir("/dev/shm")
	if err != nil {
		return
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid {
			_ = os.RemoveAll(filepath.Join("/dev/shm", e.Name()))
		}
	}
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
