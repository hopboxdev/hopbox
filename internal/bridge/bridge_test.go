package bridge_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
)

// TestCDPBridgeStartStop verifies the bridge starts a listener and stops cleanly.
func TestCDPBridgeStartStop(t *testing.T) {
	b := bridge.NewCDPBridge("127.0.0.1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- b.Start(ctx)
	}()

	// Give it time to bind.
	time.Sleep(50 * time.Millisecond)

	if !strings.Contains(b.Status(), "running") {
		t.Errorf("status = %q, want to contain 'running'", b.Status())
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("bridge did not stop after context cancel")
	}

	if strings.Contains(b.Status(), "running") {
		t.Error("status should not be 'running' after stop")
	}
}

// TestCDPBridgeProxiesConnection verifies that the CDP bridge forwards a
// connection to the local target port.
func TestCDPBridgeProxiesConnection(t *testing.T) {
	// Start a "chrome" stub that echoes back what it receives.
	chromeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen chrome stub: %v", err)
	}
	defer chromeListener.Close()
	chromePort := chromeListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := chromeListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				_, _ = c.Write(buf[:n])
			}(conn)
		}
	}()

	b := bridge.NewCDPBridgeOnPort("127.0.0.1", 0, chromePort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startedCh := make(chan int, 1)
	go func() {
		_ = b.StartWithNotify(ctx, startedCh)
	}()

	// Wait for the bridge to report its listen port.
	var listenPort int
	select {
	case listenPort = <-startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not start in time")
	}

	// Connect through the bridge.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort), time.Second)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()

	msg := "hello cdp"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != msg {
		t.Errorf("echo = %q, want %q", buf, msg)
	}
}

// TestClipboardBridgeStartStop verifies the clipboard bridge starts and stops.
func TestClipboardBridgeStartStop(t *testing.T) {
	b := bridge.NewClipboardBridgeOnPort("127.0.0.1", 0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- b.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	if !strings.Contains(b.Status(), "running") {
		t.Errorf("status = %q, want to contain 'running'", b.Status())
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("bridge did not stop after context cancel")
	}
}

// TestClipboardBridgeReceivesData verifies the bridge accepts a connection
// and calls the clipboard writer with the received bytes.
func TestClipboardBridgeReceivesData(t *testing.T) {
	var written []byte
	fakeWriter := func(data []byte) error {
		written = append(written, data...)
		return nil
	}

	b := bridge.NewClipboardBridgeOnPort("127.0.0.1", 0)
	b.SetClipboardWriter(fakeWriter)

	startedCh := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = b.StartWithNotify(ctx, startedCh)
	}()

	var listenPort int
	select {
	case listenPort = <-startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not start in time")
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort), time.Second)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	msg := "clipboard content"
	_, _ = conn.Write([]byte(msg))
	conn.Close()

	// Give the handler goroutine time to process.
	time.Sleep(100 * time.Millisecond)

	if string(written) != msg {
		t.Errorf("clipboard received %q, want %q", written, msg)
	}
}
