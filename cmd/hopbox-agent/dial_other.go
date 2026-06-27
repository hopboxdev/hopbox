//go:build !linux

package main

// setUserTimeout is a no-op off Linux (the agent runs in a Linux box; this keeps
// non-Linux builds compiling).
func setUserTimeout(_, _ int) error { return nil }
