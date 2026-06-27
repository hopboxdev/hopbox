package main

import (
	"net"
	"syscall"
	"time"
)

// dial connects to the control plane with an aggressive TCP_USER_TIMEOUT (on
// Linux) so a dead connection is abandoned within seconds even when no RST comes
// back — the case after a suspend/restore, where the pre-snapshot socket is a
// zombie and the host drops its stale-sequence packets. Paired with the short
// yamux keepalive (which keeps unacked data in flight), this gives the agent a
// fast reconnect, hence a fast wake.
func dial(addr string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if cerr := c.Control(func(fd uintptr) { serr = setUserTimeout(int(fd), 8000) }); cerr != nil {
				return cerr
			}
			return serr
		},
	}
	return d.Dial("tcp", addr)
}
