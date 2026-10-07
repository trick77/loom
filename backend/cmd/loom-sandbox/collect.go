package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// collectOutputs reads the files the job left in dir. The job is untrusted and
// already dead, but its directory is not: dir itself is opened without
// following a symlink, entries are opened relative to that handle without
// following symlinks or blocking on a FIFO, and only single-link regular files
// with a safe name and an allowed extension come back. Everything else is named
// in dropped so the model learns why a file did not arrive. A missing or
// replaced out/ means no files, never a failed run.
func collectOutputs(dir string) (files []wireFile, dropped []string, err error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, []string{"out/ is missing or not a directory; no files delivered"}, nil
	}
	d := os.NewFile(uintptr(fd), dir) //nolint:gosec // fd comes from a successful open
	defer func() { _ = d.Close() }()
	// A job can create any number of empty files; look at a bounded number so
	// a flood cannot blow up the server's memory or the response.
	entries, err := d.ReadDir(maxOutputEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	more := len(entries) > maxOutputEntries
	if more {
		entries = entries[:maxOutputEntries]
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	defer func() {
		if len(dropped) > maxDroppedReported {
			rest := len(dropped) - maxDroppedReported
			dropped = append(dropped[:maxDroppedReported], fmt.Sprintf("%d more files not delivered", rest))
		}
		if more {
			dropped = append(dropped, fmt.Sprintf("more than %d entries in out/; the rest were not examined", maxOutputEntries))
		}
	}()
	total := 0
	for _, e := range entries {
		name := e.Name()
		reason := ""
		switch {
		case !validName(name):
			reason = "unsupported file name"
		case !allowedOutputExt[strings.ToLower(filepath.Ext(name))]:
			reason = "file type not allowed (png, csv, xlsx, json, txt, md)"
		case len(files) >= maxOutputFiles:
			reason = fmt.Sprintf("more than %d files", maxOutputFiles)
		}
		if reason != "" {
			dropped = append(dropped, displayName(name)+": "+reason)
			continue
		}
		data, why := readRegularFile(fd, name, maxOutputBytes-total)
		if why != "" {
			dropped = append(dropped, name+": "+why)
			continue
		}
		total += len(data)
		files = append(files, wireFile{Name: name, Data: data})
	}
	return files, dropped, nil
}

// readRegularFile opens name inside the directory dirFD, never following a
// symlink and never blocking on a FIFO.
func readRegularFile(dirFD int, name string, budget int) ([]byte, string) {
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "not a regular file"
	}
	f := os.NewFile(uintptr(fd), name) //nolint:gosec // fd comes from a successful openat
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, "not a regular file"
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		return nil, "hard links are not allowed"
	}
	if fi.Size() > int64(budget) {
		return nil, "exceeds the total output size limit"
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(budget)+1))
	if err != nil {
		return nil, "unreadable"
	}
	if len(data) > budget {
		return nil, "exceeds the total output size limit"
	}
	return data, ""
}

// displayName keeps a rejected name printable and short in the report.
func displayName(name string) string {
	r := []rune(name)
	if len(r) > 60 {
		r = append(r[:60], '…')
	}
	return fmt.Sprintf("%q", string(r))
}
