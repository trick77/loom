package sidecar

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/fsx"
	"github.com/trick77/ship/kit/httpx"
)

// Main is a sidecar binary's main: "serve" runs serve, "healthcheck" probes
// its /healthz (the container's own healthcheck, distroless has no curl).
// It exits with the command's status.
func Main(name, addrEnv, defaultAddr string, serve func() error) {
	os.Exit(dispatch(name, addrEnv, defaultAddr, os.Args[1:], os.Getenv, os.Stderr, serve))
}

// dispatch is Main without the exit: 2 on a missing command, 1 on a failed
// or unknown one.
func dispatch(name, addrEnv, defaultAddr string, args []string, getenv func(string) string, stderr io.Writer, serve func() error) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintf(stderr, "usage: %s serve|healthcheck\n", name)
		return 2
	}
	var err error
	switch args[0] {
	case "serve":
		err = serve()
	case "healthcheck":
		// Read like serve reads it (trimmed, empty is unset), or the probe
		// would miss a server listening fine.
		return httpx.Healthcheck(httpx.LocalURL(env.New(getenv).String(addrEnv, defaultAddr), "/healthz"))
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

// Writable fails the start when dir cannot be written: a host dir Docker
// created for a bind mount is root-owned, and the sidecar would run healthy
// with no state and fail only on the first save.
func Writable(dir string) error {
	p := filepath.Join(dir, ".write-check")
	if err := fsx.WriteFileAtomic(p, nil, 0o600); err != nil {
		return fmt.Errorf("data dir %s is not writable: on the host, chown 65532:65532 it: %w", dir, err)
	}
	return os.Remove(p)
}
