// Command hopbox-mcp is the hopbox AI-control plane as an MCP server
// (design/ai-control-protocol.md): a subscribable hopbox://fleet resource,
// box.delegate/box.spawn/fleet.get tools, and pushed change notifications — the
// event-driven, no-poll model.
//
//	hopbox-mcp                 # serve MCP over stdio; spawn boxes via ssh (standalone)
//	hopbox-mcp --demo          # self-drive a demo against box.hopbox.dev
//	hopbox-mcp --connect ADDR  # bridge stdio <-> a daemon MCP socket (unix:/path or host:port)
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"github.com/hopboxdev/hopbox/internal/mcp"
)

func main() {
	demo := flag.Bool("demo", false, "self-drive a demo over an in-memory MCP pipe")
	host := flag.String("host", "box.hopbox.dev", "boxd front door for standalone/demo boxes")
	connect := flag.String("connect", "", "bridge stdio to a daemon MCP socket (unix:/path or host:port)")
	flag.Parse()

	if *demo {
		runDemoMode(*connect, *host)
		return
	}
	if *connect != "" {
		bridge(*connect)
		return
	}
	be := newSSHBackend(*host)
	defer be.cleanup()
	mcp.NewServer(be).Serve(os.Stdin, os.Stdout)
}

// bridge pipes local stdio to a daemon's MCP socket, so an AI's stdio MCP client
// can drive the in-daemon control plane (which has the real engine + fleet).
func bridge(addr string) {
	network, a := "tcp", addr
	if s, ok := strings.CutPrefix(addr, "unix:"); ok {
		network, a = "unix", s
	}
	c, err := net.Dial(network, a)
	if err != nil {
		log.Fatalf("connect %s: %v", addr, err)
	}
	defer c.Close()
	go func() {
		_, _ = io.Copy(c, os.Stdin)
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	_, _ = io.Copy(os.Stdout, c)
}
