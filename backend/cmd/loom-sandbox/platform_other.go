//go:build !linux

package main

import "errors"

const childCommand = "exec-child"

var errLinuxOnly = errors.New("the sandbox runs on Linux only")

func newExecutor(config) (executor, error) { return nil, errLinuxOnly }

func execChild([]string) error { return errLinuxOnly }
