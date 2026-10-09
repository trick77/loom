package turntest

import (
	"context"
	"time"

	"github.com/trick77/loom/internal/sandbox"
)

// Sandbox is a sandbox runner that returns Result and Err and records every
// request in Got.
type Sandbox struct {
	Enabled bool
	Result  sandbox.Result
	Err     error
	Got     []sandbox.Request
}

// Available implements turn.SandboxRunner.
func (f *Sandbox) Available() bool { return f.Enabled }

// Timeout implements turn.SandboxRunner.
func (f *Sandbox) Timeout() time.Duration { return time.Minute }

// Run implements turn.SandboxRunner.
func (f *Sandbox) Run(_ context.Context, r sandbox.Request) (sandbox.Result, error) {
	f.Got = append(f.Got, r)
	return f.Result, f.Err
}
