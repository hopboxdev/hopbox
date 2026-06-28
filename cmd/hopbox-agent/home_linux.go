//go:build linux

package main

import "golang.org/x/sys/unix"

// mountDev mounts an ext4 block device at target via the mount(2) syscall.
func mountDev(dev, target string) error {
	return unix.Mount(dev, target, "ext4", 0, "")
}
