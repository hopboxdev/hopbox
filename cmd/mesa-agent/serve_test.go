package main

import (
	"io"
	"net"
	"runtime"
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

func TestHandleStreamNoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	c1, c2 := net.Pipe()
	agentSess, err := yamux.Server(c1, nil)
	if err != nil {
		t.Fatal(err)
	}
	go serveSession(agentSess)
	ctrlSess, err := yamux.Client(c2, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := ctrlSess.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := agentproto.WriteShellHeader(st, agentproto.ShellHeader{Cmd: "/bin/sh -c 'echo hi'"}); err != nil {
		t.Fatal(err)
	}
	// drain output so the pty->controller copy completes and the shell is reaped
	buf := make([]byte, 64)
	_, _ = st.Read(buf)
	// keep the controller stream OPEN (the leak condition), give the agent time to exit
	time.Sleep(500 * time.Millisecond)
	// allow async teardown; poll a few times
	// Two open yamux sessions (agent + ctrl) each spawn ~3-4 goroutines
	// for keepalive/send/recv; that accounts for the base+7 steady state.
	// We allow base+8 to give one extra slot of margin while still catching
	// a leaked controller->pty copy goroutine (which would push delta to 10+).
	leaked := true
	for i := 0; i < 20; i++ {
		if runtime.NumGoroutine() <= base+8 { // tolerance for yamux internals (two open sessions = ~+7)
			leaked = false
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if leaked {
		t.Fatalf("goroutine count did not return near baseline: base=%d now=%d", base, runtime.NumGoroutine())
	}
	_ = st.Close()
	_ = ctrlSess.Close()
	_ = agentSess.Close()
}
