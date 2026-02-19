package tunnel

import (
	"context"
	"io"
	"net"
	"testing"
)

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestProxyDataRoundTrip(t *testing.T) {
	echoAddr := startEchoServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proxy, err := StartProxy(ctx, ProxyConfig{
		LocalAddr:  "127.0.0.1:0",
		RemoteAddr: echoAddr,
		Label:      "test-echo",
	}, (&net.Dialer{}).DialContext)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer proxy.Stop()

	conn, err := net.Dial("tcp", proxy.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	msg := []byte("hello proxy")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("got %q, want %q", buf, msg)
	}
}

func TestProxyStop(t *testing.T) {
	echoAddr := startEchoServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proxy, err := StartProxy(ctx, ProxyConfig{
		LocalAddr:  "127.0.0.1:0",
		RemoteAddr: echoAddr,
		Label:      "test-stop",
	}, (&net.Dialer{}).DialContext)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}

	localAddr := proxy.LocalAddr().String()
	proxy.Stop()

	_, err = net.Dial("tcp", localAddr)
	if err == nil {
		t.Error("expected connection to fail after Stop()")
	}
}
