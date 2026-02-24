package bridge

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/service"
)

// startEchoServer starts a TCP echo server on the given address.
func startEchoServer(t *testing.T, addr string) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen echo server: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
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
	return ln, port
}

func TestPortForwarderDiscoverAndProxy(t *testing.T) {
	// Echo server on IPv6 loopback; forwarder proxy on 127.0.0.1 (IPv4).
	echoLn, echoPort := startEchoServer(t, "[::1]:0")
	defer func() { _ = echoLn.Close() }()

	forwarded := make(chan int, 4)

	pf := NewPortForwarder("test", "::1",
		WithInterval(50*time.Millisecond),
		WithOnForward(func(port int) { forwarded <- port }),
		withListPorts(func() ([]service.ListeningPort, error) {
			return []service.ListeningPort{{Port: echoPort}}, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = pf.Run(ctx) }()

	select {
	case p := <-forwarded:
		if p != echoPort {
			t.Fatalf("forwarded port = %d, want %d", p, echoPort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("port was not forwarded in time")
	}

	// Connect through the proxy at 127.0.0.1:PORT → [::1]:PORT.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort), time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer func() { _ = conn.Close() }()

	msg := "hello port forward"
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

	ports := pf.Ports()
	if len(ports) != 1 || ports[0] != echoPort {
		t.Errorf("Ports() = %v, want [%d]", ports, echoPort)
	}
}

func TestPortForwarderRemovesStaleProxy(t *testing.T) {
	echoLn, echoPort := startEchoServer(t, "[::1]:0")
	defer func() { _ = echoLn.Close() }()

	var mu sync.Mutex
	remotePorts := []service.ListeningPort{{Port: echoPort}}

	forwarded := make(chan int, 4)
	unforwarded := make(chan int, 4)

	pf := NewPortForwarder("test", "::1",
		WithInterval(50*time.Millisecond),
		WithOnForward(func(port int) { forwarded <- port }),
		WithOnUnforward(func(port int) { unforwarded <- port }),
		withListPorts(func() ([]service.ListeningPort, error) {
			mu.Lock()
			defer mu.Unlock()
			out := make([]service.ListeningPort, len(remotePorts))
			copy(out, remotePorts)
			return out, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = pf.Run(ctx) }()

	select {
	case <-forwarded:
	case <-time.After(2 * time.Second):
		t.Fatal("port was not forwarded in time")
	}

	mu.Lock()
	remotePorts = nil
	mu.Unlock()

	select {
	case p := <-unforwarded:
		if p != echoPort {
			t.Fatalf("unforwarded port = %d, want %d", p, echoPort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("port was not unforwarded in time")
	}

	if ports := pf.Ports(); len(ports) != 0 {
		t.Errorf("Ports() = %v, want empty", ports)
	}
}

func TestPortForwarderExcludedPorts(t *testing.T) {
	forwarded := make(chan int, 4)

	pf := NewPortForwarder("test", "::1",
		WithInterval(50*time.Millisecond),
		WithOnForward(func(port int) { forwarded <- port }),
		withListPorts(func() ([]service.ListeningPort, error) {
			return []service.ListeningPort{
				{Port: 22},
				{Port: 4200},
				{Port: 51820},
			}, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = pf.Run(ctx) }()

	time.Sleep(200 * time.Millisecond)

	if ports := pf.Ports(); len(ports) != 0 {
		t.Errorf("excluded ports should not be forwarded, got %v", ports)
	}

	select {
	case p := <-forwarded:
		t.Errorf("should not have forwarded port %d", p)
	default:
	}
}

func TestPortForwarderSkipsLocallyUsedPorts(t *testing.T) {
	// Occupy a port locally on 127.0.0.1.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	occupiedPort := ln.Addr().(*net.TCPAddr).Port

	forwarded := make(chan int, 4)

	pf := NewPortForwarder("test", "::1",
		WithInterval(50*time.Millisecond),
		WithOnForward(func(port int) { forwarded <- port }),
		withListPorts(func() ([]service.ListeningPort, error) {
			return []service.ListeningPort{{Port: occupiedPort}}, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = pf.Run(ctx) }()

	time.Sleep(200 * time.Millisecond)

	select {
	case p := <-forwarded:
		t.Errorf("should not have forwarded occupied port %d", p)
	default:
	}
}

func TestPortForwarderStopCleansUp(t *testing.T) {
	echoLn, echoPort := startEchoServer(t, "[::1]:0")
	defer func() { _ = echoLn.Close() }()

	forwarded := make(chan int, 4)

	pf := NewPortForwarder("test", "::1",
		WithInterval(50*time.Millisecond),
		WithOnForward(func(port int) { forwarded <- port }),
		withListPorts(func() ([]service.ListeningPort, error) {
			return []service.ListeningPort{{Port: echoPort}}, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())

	go func() { _ = pf.Run(ctx) }()

	select {
	case <-forwarded:
	case <-time.After(2 * time.Second):
		t.Fatal("port was not forwarded in time")
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	if ports := pf.Ports(); len(ports) != 0 {
		t.Errorf("after Stop, Ports() = %v, want empty", ports)
	}
}
