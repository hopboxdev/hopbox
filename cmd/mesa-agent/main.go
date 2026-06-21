// Command mesa-agent runs inside every workspace. It dials OUT to mesad, proves
// its one-time bootstrap token, and serves a yamux session; each incoming
// stream becomes a pty-backed shell. The control plane never routes INTO it.
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/hashicorp/yamux"

	"github.com/mesadev/mesa/internal/agentproto"
)

func main() {
	addr := os.Getenv("MESA_CONTROL_ADDR")
	token := os.Getenv("MESA_AGENT_TOKEN")
	wsID := os.Getenv("MESA_WORKSPACE_ID")
	if addr == "" || token == "" {
		log.Fatal("mesa-agent: MESA_CONTROL_ADDR and MESA_AGENT_TOKEN are required")
	}
	for {
		if err := connectAndServe(addr, agentproto.Handshake{WorkspaceID: wsID, Token: token}); err != nil {
			log.Printf("mesa-agent: connection ended: %v; retrying in 2s", err)
		}
		time.Sleep(2 * time.Second) // reconnect with simple backoff
	}
}

func connectAndServe(addr string, hs agentproto.Handshake) error {
	conn, err := dial(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := agentproto.WriteHandshake(conn, hs); err != nil {
		return err
	}
	sess, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}
	log.Printf("mesa-agent: connected to %s, serving session", addr)
	return serveSession(sess)
}

// serveSession accepts yamux streams until the session closes.
func serveSession(sess *yamux.Session) error {
	for {
		stream, err := sess.Accept()
		if err != nil {
			return err
		}
		go handleStream(stream)
	}
}

// handleStream reads the OpenFrame and dispatches to the shell or forward handler.
func handleStream(stream io.ReadWriteCloser) {
	defer stream.Close()
	of, err := agentproto.ReadOpenFrame(stream)
	if err != nil {
		log.Printf("mesa-agent: read open frame: %v", err)
		return
	}
	switch of.Kind {
	case agentproto.KindForward:
		handleForward(stream)
	default: // KindShell
		handleShell(stream)
	}
}

// handleForward dials a local TCP service in the workspace and pipes the stream
// to it (mesa-gw -> agent -> localhost:port). This is how an exposed workspace
// service is reached from the gateway.
func handleForward(stream io.ReadWriteCloser) {
	hdr, err := agentproto.ReadForwardHeader(stream)
	if err != nil {
		log.Printf("mesa-agent: read forward header: %v", err)
		return
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", hdr.Port))
	if err != nil {
		log.Printf("mesa-agent: forward dial 127.0.0.1:%d: %v", hdr.Port, err)
		return
	}
	defer conn.Close()
	go func() { _, _ = io.Copy(conn, stream) }() // gateway -> service
	_, _ = io.Copy(stream, conn)                 // service -> gateway
}

// handleShell reads a ShellHeader, then bridges a pty to the stream.
func handleShell(stream io.ReadWriteCloser) {
	hdr, err := agentproto.ReadShellHeader(stream)
	if err != nil {
		log.Printf("mesa-agent: read shell header: %v", err)
		return
	}
	cmd := buildCommand(hdr.Cmd)
	ws := &pty.Winsize{Cols: hdr.Cols, Rows: hdr.Rows}
	if ws.Cols == 0 {
		ws.Cols, ws.Rows = 80, 24
	}
	f, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		log.Printf("mesa-agent: pty start: %v", err)
		return
	}
	defer func() { _ = f.Close() }()

	go func() { _, _ = io.Copy(f, stream) }() // controller -> pty
	_, _ = io.Copy(stream, f)                 // pty -> controller
	_ = cmd.Wait()

	// The shell has exited. Force the controller->pty copy goroutine to
	// unblock: yamux treats our local Close() as "read normally" (not EOF),
	// so a parked Read only returns on a remote FIN or a forced deadline.
	if d, ok := stream.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = d.SetReadDeadline(time.Now())
	}
}

func buildCommand(spec string) *exec.Cmd {
	if spec == "" {
		spec = "/bin/bash"
	}
	// M1: support a bare program or a "/bin/sh -c '...'" form via sh.
	var c *exec.Cmd
	if strings.Contains(spec, " ") {
		c = exec.Command("/bin/sh", "-c", spec)
	} else {
		c = exec.Command(spec)
	}
	c.Env = append(os.Environ(), "TERM=xterm-256color")
	return c
}
