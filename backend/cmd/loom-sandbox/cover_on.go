//go:build sandboxcover

package main

import (
	"os"
	"runtime/coverage"
)

// The CI escape test runs a -cover build with this tag, so the code that only
// runs as root inside the container is measured too. A -cover binary writes
// its counters only on a clean exit, which neither the server (stopped by a
// signal) nor the child (replaced by exec) ever reaches; both flush
// explicitly instead.
func coverageFlush() {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		return
	}
	_ = coverage.WriteMetaDir(dir)
	_ = coverage.WriteCountersDir(dir)
}

// childEnv passes the coverage directory on to exec-child, whose environment
// is otherwise empty.
func childEnv() []string {
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		return []string{"GOCOVERDIR=" + dir}
	}
	return []string{}
}
