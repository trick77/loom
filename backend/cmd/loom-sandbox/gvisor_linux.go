//go:build linux

package main

import (
	"errors"
	"os"
)

func requireGVisor() error {
	b, err := os.ReadFile("/proc/version")
	if err != nil {
		return err
	}
	if !isGVisorVersion(string(b)) {
		return errors.New("not running under gVisor (runtime: runsc); set SANDBOX_INSECURE_DEV=1 only for local development")
	}
	return nil
}
