// Command loom-sandbox runs untrusted Python for loom's run_python tool.
//
// It is the only process in the sandbox container. `serve` takes jobs over
// HTTP; each job runs as a re-exec of this binary (`exec-child`) in fresh
// network, PID, mount, IPC and UTS namespaces, which drops to an unprivileged
// slot uid and then becomes the Python interpreter. The container must run
// under gVisor (runsc): the namespaces and rlimits are defence in depth, gVisor
// is the boundary to the host kernel.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-sandbox serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		err = healthcheck(envOr("SANDBOX_ADDR", defaultAddr))
	case childCommand:
		// Only reached in a fresh namespace set; on success it never returns.
		err = execChild(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-sandbox:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.insecureDev {
		if err := requireGVisor(); err != nil {
			return err
		}
	} else {
		slog.Warn("SANDBOX_INSECURE_DEV is set: running without gVisor, for local development only")
	}
	exec, err := newExecutor(cfg)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newServer(cfg, exec),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("sandbox listening", "addr", cfg.addr, "slots", cfg.slots)
	return srv.ListenAndServe()
}

func healthcheck(addr string) error {
	host := addr
	if len(host) > 0 && host[0] == ':' {
		host = "127.0.0.1" + host
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
