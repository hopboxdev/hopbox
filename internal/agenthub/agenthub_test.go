package agenthub_test

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"

	"github.com/hashicorp/yamux"

	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/agentproto"
)

func TestOpenShellReachesAgent(t *testing.T) {
	c1, c2 := net.Pipe()

	// fake agent: yamux server that echoes the shell header's Cmd back as data
	go func() {
		sess, err := yamux.Server(c1, nil)
		if err != nil {
			return
		}
		st, err := sess.Accept()
		if err != nil {
			return
		}
		of, _ := agentproto.ReadOpenFrame(st)
		hdr, _ := agentproto.ReadShellHeader(st)
		_, _ = io.WriteString(st, of.Kind+":"+hdr.Cmd)
		_ = st.Close()
	}()

	hub := agenthub.New()
	clientSess, _ := yamux.Client(c2, nil)
	hub.Register("w1", clientSess)

	stream, err := hub.OpenShell(context.Background(), "w1", agentproto.ShellHeader{Cmd: "/bin/bash"})
	if err != nil {
		t.Fatalf("openshell: %v", err)
	}
	defer stream.Close()
	b, _ := io.ReadAll(stream)
	if string(b) != "shell:/bin/bash" {
		t.Fatalf("got %q", string(b))
	}
}

func TestOpenForwardReachesAgent(t *testing.T) {
	c1, c2 := net.Pipe()

	// fake agent: read the open frame + forward header, echo the requested port.
	go func() {
		sess, err := yamux.Server(c1, nil)
		if err != nil {
			return
		}
		st, err := sess.Accept()
		if err != nil {
			return
		}
		of, _ := agentproto.ReadOpenFrame(st)
		fh, _ := agentproto.ReadForwardHeader(st)
		_, _ = io.WriteString(st, of.Kind+":"+itoa(fh.Port))
		_ = st.Close()
	}()

	hub := agenthub.New()
	clientSess, _ := yamux.Client(c2, nil)
	hub.Register("w1", clientSess)

	conn, err := hub.OpenForward("w1", 3000)
	if err != nil {
		t.Fatalf("openforward: %v", err)
	}
	defer conn.Close()
	b, _ := io.ReadAll(conn)
	if string(b) != "forward:3000" {
		t.Fatalf("got %q", string(b))
	}
}

func itoa(p uint32) string { return strconv.FormatUint(uint64(p), 10) }

func TestOpenExecReachesAgent(t *testing.T) {
	c1, c2 := net.Pipe()

	// fake agent: read the open frame + exec header, echo the command joined.
	go func() {
		sess, err := yamux.Server(c1, nil)
		if err != nil {
			return
		}
		st, err := sess.Accept()
		if err != nil {
			return
		}
		of, _ := agentproto.ReadOpenFrame(st)
		eh, _ := agentproto.ReadExecHeader(st)
		_, _ = io.WriteString(st, of.Kind+":"+eh.Cmd[0])
		_ = st.Close()
	}()

	hub := agenthub.New()
	clientSess, _ := yamux.Client(c2, nil)
	hub.Register("w1", clientSess)

	stream, err := hub.OpenExec("w1", []string{"ls", "-la"})
	if err != nil {
		t.Fatalf("openexec: %v", err)
	}
	defer stream.Close()
	b, _ := io.ReadAll(stream)
	if string(b) != "exec:ls" {
		t.Fatalf("got %q", string(b))
	}
}

func TestOpenShellUnknownWorkspace(t *testing.T) {
	hub := agenthub.New()
	if _, err := hub.OpenShell(context.Background(), "ghost", agentproto.ShellHeader{}); err == nil {
		t.Fatal("expected error for unconnected workspace")
	}
}

func TestConnectedReflectsRegistration(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	_, _ = yamux.Server(c1, nil)
	sess, _ := yamux.Client(c2, nil)

	hub := agenthub.New()
	if hub.Connected("w1") {
		t.Fatal("should not be connected yet")
	}
	hub.Register("w1", sess)
	if !hub.Connected("w1") {
		t.Fatal("should be connected after register")
	}
	hub.Unregister("w1")
	if hub.Connected("w1") {
		t.Fatal("should be disconnected after unregister")
	}
}
