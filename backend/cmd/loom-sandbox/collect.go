package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// collectOutputs reads the files the job left in dir. The job is untrusted and
// already dead, but its directory is not: entries are opened without following
// symlinks and without blocking on a FIFO, and only single-link regular files
// with a safe name and an allowed extension come back. Everything else is named
// in dropped so the model learns why a file did not arrive.
func collectOutputs(dir string) (files []wireFile, dropped []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
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
		data, why := readRegularFile(filepath.Join(dir, name), maxOutputBytes-total)
		if why != "" {
			dropped = append(dropped, name+": "+why)
			continue
		}
		total += len(data)
		files = append(files, wireFile{Name: name, Data: data})
	}
	return files, dropped, nil
}

func readRegularFile(path string, budget int) ([]byte, string) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // path is the job's out dir plus a name that passed validName; O_NOFOLLOW refuses a symlink
	if err != nil {
		return nil, "not a regular file"
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, "not a regular file"
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && uint64(st.Nlink) != 1 {
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
