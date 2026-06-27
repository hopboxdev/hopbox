// Command hopbox-agent runs inside every workspace. It dials OUT to hopboxd, proves
// its one-time bootstrap token, and serves a yamux session; each incoming
// stream becomes a pty-backed shell. The control plane never routes INTO it.
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hashicorp/yamux"

	"github.com/hopboxdev/hopbox/internal/agentproto"
)

func main() {
	addr := os.Getenv("HOPBOX_CONTROL_ADDR")
	token := os.Getenv("HOPBOX_AGENT_TOKEN")
	wsID := os.Getenv("HOPBOX_WORKSPACE_ID")
	if addr == "" || token == "" {
		log.Fatal("hopbox-agent: HOPBOX_CONTROL_ADDR and HOPBOX_AGENT_TOKEN are required")
	}
	loadSSHConfig() // host key + authorized keys for the embedded SSH server
	if meta := os.Getenv("BOX_META"); meta != "" {
		go heartbeatLoop(meta) // F3: report load to the metadata API for idle detection
	}
	for {
		if err := connectAndServe(addr, agentproto.Handshake{WorkspaceID: wsID, Token: token}); err != nil {
			log.Printf("hopbox-agent: connection ended: %v; retrying in 2s", err)
		}
		time.Sleep(2 * time.Second) // reconnect with simple backoff
	}
}

// heartbeatLoop reports the box's 1-minute load average to the metadata API
// every 15s, so the control plane can detect idleness (F3). Identity is the
// source IP; no credential is sent.
func heartbeatLoop(base string) {
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		time.Sleep(15 * time.Second)
		body := fmt.Sprintf(`{"load":%g}`, loadAvg1())
		req, err := http.NewRequest(http.MethodPost, base+"/v1/me/heartbeat", strings.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}
}

// loadAvg1 reads the 1-minute load average from /proc/loadavg (0 if unavailable).
func loadAvg1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
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
	// Short keepalive so a dead control connection is detected within seconds —
	// notably after a suspend/restore, where the old session is frozen and the
	// peer is gone; the agent then reconnects promptly for a fast wake.
	yc := yamux.DefaultConfig()
	yc.KeepAliveInterval = 5 * time.Second
	yc.ConnectionWriteTimeout = 5 * time.Second
	sess, err := yamux.Server(conn, yc)
	if err != nil {
		return err
	}
	log.Printf("hopbox-agent: connected to %s, serving session", addr)
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
		log.Printf("hopbox-agent: read open frame: %v", err)
		return
	}
	switch of.Kind {
	case agentproto.KindForward:
		handleForward(stream)
	case agentproto.KindExec:
		handleExec(stream)
	case agentproto.KindSSH:
		handleSSH(stream)
	default: // KindShell
		handleShell(stream)
	}
}

// execWriter frames each Write as an exec stdout/stderr frame, chunking to stay
// under the frame cap. Writes are serialized via mu so stdout and stderr don't
// interleave a single frame.
type execWriter struct {
	w   io.Writer
	typ byte
	mu  *sync.Mutex
}

const execChunk = 32 * 1024

func (e *execWriter) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > execChunk {
			n = execChunk
		}
		if err := agentproto.WriteExecData(e.w, e.typ, p[:n]); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}

// handleExec runs an argv command without a pty and streams stdout/stderr back
// as exec frames, then the exit code. No stdin in v1.
func handleExec(stream io.ReadWriteCloser) {
	hdr, err := agentproto.ReadExecHeader(stream)
	if err != nil {
		log.Printf("hopbox-agent: read exec header: %v", err)
		return
	}
	if len(hdr.Cmd) == 0 {
		_ = agentproto.WriteExecExit(stream, 2)
		return
	}
	var mu sync.Mutex
	cmd := exec.Command(hdr.Cmd[0], hdr.Cmd[1:]...)
	cmd.Env = append(os.Environ(), "TERM=dumb")
	cmd.Stdout = &execWriter{w: stream, typ: agentproto.ExecStdout, mu: &mu}
	cmd.Stderr = &execWriter{w: stream, typ: agentproto.ExecStderr, mu: &mu}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = agentproto.WriteExecExit(stream, 126)
		return
	}

	if err := cmd.Start(); err != nil {
		// nothing else writes yet; emit directly.
		_ = stdin.Close()
		_ = agentproto.WriteExecData(stream, agentproto.ExecStderr, []byte("hopbox-agent: "+err.Error()+"\n"))
		_ = agentproto.WriteExecExit(stream, 127)
		return
	}

	// stdin pump: controller -> cmd, until a stdin-close frame or the stream ends.
	go func() {
		defer stdin.Close()
		for {
			typ, data, _, rerr := agentproto.ReadExecFrame(stream)
			if rerr != nil {
				return
			}
			switch typ {
			case agentproto.ExecStdin:
				if _, werr := stdin.Write(data); werr != nil {
					return
				}
			case agentproto.ExecStdinClose:
				return
			}
		}
	}()
	code := int32(0)
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = int32(ee.ExitCode())
		} else {
			code = 1
		}
	}
	mu.Lock()
	_ = agentproto.WriteExecExit(stream, code)
	mu.Unlock()
}

// handleForward dials a local TCP service in the workspace and pipes the stream
// to it (hopbox-gw -> agent -> localhost:port). This is how an exposed workspace
// service is reached from the gateway.
func handleForward(stream io.ReadWriteCloser) {
	hdr, err := agentproto.ReadForwardHeader(stream)
	if err != nil {
		log.Printf("hopbox-agent: read forward header: %v", err)
		return
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", hdr.Port))
	if err != nil {
		log.Printf("hopbox-agent: forward dial 127.0.0.1:%d: %v", hdr.Port, err)
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
		log.Printf("hopbox-agent: read shell header: %v", err)
		return
	}
	cmd := buildCommand(hdr.Cmd)
	ws := &pty.Winsize{Cols: hdr.Cols, Rows: hdr.Rows}
	if ws.Cols == 0 {
		ws.Cols, ws.Rows = 80, 24
	}
	f, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		log.Printf("hopbox-agent: pty start: %v", err)
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
	// Open the shell in the workspace home (the persistent volume), not the
	// container root `/` — otherwise the user lands somewhere their work isn't
	// even saved. Set HOME to match so `~` and shell startup resolve there.
	home := workspaceHome()
	c.Dir = home
	c.Env = append(envWithout(os.Environ(), "HOME"), "TERM=xterm-256color", "HOME="+home)
	return c
}

// workspaceHome is the persistent home mount (/home/dev) when present, else "/".
func workspaceHome() string {
	if fi, err := os.Stat("/home/dev"); err == nil && fi.IsDir() {
		return "/home/dev"
	}
	return "/"
}

// envWithout returns env with any KEY= entries removed, so a replacement can be
// appended without a duplicate.
func envWithout(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}
