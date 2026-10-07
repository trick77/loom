//go:build !sandboxcover

package main

func coverageFlush() {}

// childEnv is the environment exec-child starts with: nothing. It builds the
// interpreter's environment itself.
func childEnv() []string { return []string{} }
