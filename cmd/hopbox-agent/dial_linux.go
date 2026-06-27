package main

import "golang.org/x/sys/unix"

// setUserTimeout sets TCP_USER_TIMEOUT (ms): abandon the connection after this
// long with unacknowledged data, regardless of RST.
func setUserTimeout(fd, ms int) error {
	return unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, ms)
}
