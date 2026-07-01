package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"strings"

	"github.com/hopboxdev/hopbox/internal/mcp"
)

// openTransport returns an MCP byte transport (w to send, r to receive) + a close
// func. connect != "" dials a daemon socket (unix:/path or host:port); otherwise
// it runs a standalone ssh backend over an in-memory pipe.
func openTransport(connect, host string) (w io.Writer, r io.Reader, closeFn func()) {
	if connect != "" {
		network, a := "tcp", connect
		if s, ok := strings.CutPrefix(connect, "unix:"); ok {
			network, a = "unix", s
		}
		c, err := net.Dial(network, a)
		if err != nil {
			log.Fatalf("connect %s: %v", connect, err)
		}
		return c, c, func() { _ = c.Close() }
	}
	be := newSSHBackend(host)
	csr, csw := io.Pipe()
	scr, scw := io.Pipe()
	go mcp.NewServer(be).Serve(csr, scw)
	return csw, scr, be.cleanup
}

// route reads MCP messages from r, sending responses and notifications to separate
// channels so a client blocks on the event stream and never polls.
func route(r io.Reader, responses, notifs chan<- map[string]any) {
	dec := json.NewDecoder(r)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			close(responses)
			return
		}
		if m["method"] != nil {
			notifs <- m
		} else {
			responses <- m
		}
	}
}
