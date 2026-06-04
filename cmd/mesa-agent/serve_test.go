package main

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/mesadev/mesa/internal/agentproto"
)

func TestServeSessionRunsCommand(t *testing.T) {
	c1, c2 := net.Pipe()

	// agent side: yamux server, our handler
	agentSess, err := yamux.Server(c1, nil)
	if err != nil {
		t.Fatal(err)
	}
	go serveSession(agentSess) // function under test

	// controller side: yamux client opens a stream
	ctrlSess, err := yamux.Client(c2, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := ctrlSess.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := agentproto.WriteShellHeader(st, agentproto.ShellHeader{
		Cmd: "/bin/sh -c 'echo mesa-ok'", Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(st)
		done <- string(b)
	}()

	select {
	case out := <-done:
		if !strings.Contains(out, "mesa-ok") {
			t.Fatalf("output %q missing marker", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for shell output")
	}
}
