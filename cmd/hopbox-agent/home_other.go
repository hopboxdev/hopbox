//go:build !linux

package main

import "errors"

// mountDev is linux-only; the agent only mounts a home drive inside a microVM.
func mountDev(_, _ string) error { return errors.New("home mount: linux only") }
