package bridge_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
)

// TestCDPBridgeStartStop verifies the bridge starts a listener and stops cleanly.
func TestCDPBridgeStartStop(t *testing.T) {
	// Use port 0 so the test doesn't depend on port 9222 being free.
	b := bridge.NewCDPBridgeOnPort("127.0.0.1", 0, 9222)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	startedCh := make(chan int, 1)
	go func() {
		done <- b.StartWithNotify(ctx, startedCh)
	}()

	// Wait until the listener is bound before checking status.
	select {
	case <-startedCh:
	case err := <-done:
		t.Fatalf("bridge failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not start in time")
	}

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
	defer func() { _ = chromeListener.Close() }()
	chromePort := chromeListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := chromeListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
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
	defer func() { _ = conn.Close() }()

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
	startedCh := make(chan int, 1)
	go func() {
		done <- b.StartWithNotify(ctx, startedCh)
	}()

	// Wait until the listener is bound before checking status.
	select {
	case <-startedCh:
	case err := <-done:
		t.Fatalf("bridge failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not start in time")
	}

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
	writerDone := make(chan struct{})
	fakeWriter := func(data []byte) error {
		written = append(written, data...)
		close(writerDone)
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
	_ = conn.Close()

	// Block until the handler goroutine has finished writing — a time.Sleep
	// is not a happens-before and causes a data race on `written`.
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("clipboard writer was not called")
	}

	if string(written) != msg {
		t.Errorf("clipboard received %q, want %q", written, msg)
	}
}

// TestXDGOpenBridgeStartStop verifies the xdg-open bridge starts and stops.
func TestXDGOpenBridgeStartStop(t *testing.T) {
	b := bridge.NewXDGOpenBridgeOnPort("127.0.0.1", 0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	startedCh := make(chan int, 1)
	go func() {
		done <- b.StartWithNotify(ctx, startedCh)
	}()

	select {
	case <-startedCh:
	case err := <-done:
		t.Fatalf("bridge failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not start in time")
	}

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

// TestXDGOpenBridgeOpensURL verifies the bridge calls the opener with the received URL.
func TestXDGOpenBridgeOpensURL(t *testing.T) {
	var mu sync.Mutex
	var opened string
	openerDone := make(chan struct{})
	fakeOpener := func(url string) error {
		mu.Lock()
		opened = url
		mu.Unlock()
		close(openerDone)
		return nil
	}

	b := bridge.NewXDGOpenBridgeOnPort("127.0.0.1", 0)
	b.SetOpener(fakeOpener)

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
	url := "https://example.com/test"
	_, _ = conn.Write([]byte(url + "\n"))
	_ = conn.Close()

	select {
	case <-openerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("opener was not called")
	}

	mu.Lock()
	if opened != url {
		t.Errorf("opened = %q, want %q", opened, url)
	}
	mu.Unlock()
}

// TestNotificationBridgeStartStop verifies the notification bridge starts and stops.
func TestNotificationBridgeStartStop(t *testing.T) {
	b := bridge.NewNotificationBridgeOnPort("127.0.0.1", 0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	startedCh := make(chan int, 1)
	go func() {
		done <- b.StartWithNotify(ctx, startedCh)
	}()

	select {
	case <-startedCh:
	case err := <-done:
		t.Fatalf("bridge failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not start in time")
	}

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

// TestNotificationBridgeReceivesPayload verifies the bridge calls the notifier with
// the received title and body.
func TestNotificationBridgeReceivesPayload(t *testing.T) {
	var mu sync.Mutex
	var gotTitle, gotBody string
	notifierDone := make(chan struct{})
	fakeNotifier := func(title, body string) error {
		mu.Lock()
		gotTitle = title
		gotBody = body
		mu.Unlock()
		close(notifierDone)
		return nil
	}

	b := bridge.NewNotificationBridgeOnPort("127.0.0.1", 0)
	b.SetNotifier(fakeNotifier)

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
	payload, _ := json.Marshal(map[string]string{"title": "Build Done", "body": "All tests passed"})
	_, _ = conn.Write(payload)
	_ = conn.Close()

	select {
	case <-notifierDone:
	case <-time.After(2 * time.Second):
		t.Fatal("notifier was not called")
	}

	mu.Lock()
	if gotTitle != "Build Done" {
		t.Errorf("title = %q, want %q", gotTitle, "Build Done")
	}
	if gotBody != "All tests passed" {
		t.Errorf("body = %q, want %q", gotBody, "All tests passed")
	}
	mu.Unlock()
}

// TestNotificationBridgeIgnoresInvalidJSON verifies the bridge does not crash
// when receiving invalid JSON.
func TestNotificationBridgeIgnoresInvalidJSON(t *testing.T) {
	called := make(chan struct{}, 1)
	fakeNotifier := func(_, _ string) error {
		called <- struct{}{}
		return nil
	}

	b := bridge.NewNotificationBridgeOnPort("127.0.0.1", 0)
	b.SetNotifier(fakeNotifier)

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
	_, _ = conn.Write([]byte("this is not json"))
	_ = conn.Close()

	// Give the handler a moment to process; notifier should NOT be called.
	select {
	case <-called:
		t.Error("notifier should not be called for invalid JSON")
	case <-time.After(200 * time.Millisecond):
		// expected
	}
}
