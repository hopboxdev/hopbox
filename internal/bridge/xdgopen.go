package bridge

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
)

const defaultXDGOpenPort = 2225

// XDGOpenBridge listens for URLs sent from the server and opens them in the
// local browser. The server-side `xdg-open` shim sends one URL per line over
// TCP to the client's WireGuard IP.
type XDGOpenBridge struct {
	listenAddr string
	listener   net.Listener
	running    atomic.Bool
	opener     func(string) error // injectable for tests; defaults to openURL
}

// NewXDGOpenBridge creates an xdg-open bridge that listens on the default port.
func NewXDGOpenBridge(listenIP string) *XDGOpenBridge {
	return NewXDGOpenBridgeOnPort(listenIP, defaultXDGOpenPort)
}

// NewXDGOpenBridgeOnPort creates an xdg-open bridge on a specific port.
// If port is 0, an ephemeral port is used.
func NewXDGOpenBridgeOnPort(listenIP string, port int) *XDGOpenBridge {
	return &XDGOpenBridge{
		listenAddr: fmt.Sprintf("%s:%d", listenIP, port),
		opener:     openURL,
	}
}

// SetOpener overrides the URL-opening function. Used in tests.
func (b *XDGOpenBridge) SetOpener(fn func(string) error) {
	b.opener = fn
}

// Start begins listening for xdg-open requests.
func (b *XDGOpenBridge) Start(ctx context.Context) error {
	return b.start(ctx, nil)
}

// StartWithNotify starts the bridge and sends the bound port to ch once listening.
func (b *XDGOpenBridge) StartWithNotify(ctx context.Context, ch chan<- int) error {
	return b.start(ctx, ch)
}

func (b *XDGOpenBridge) start(ctx context.Context, portNotify chan<- int) error {
	ln, err := net.Listen("tcp", b.listenAddr)
	if err != nil {
		return fmt.Errorf("xdg-open bridge listen: %w", err)
	}
	b.listener = ln
	b.running.Store(true)

	if portNotify != nil {
		portNotify <- ln.Addr().(*net.TCPAddr).Port
	}

	go b.acceptLoop(ctx)

	<-ctx.Done()
	b.Stop()
	return nil
}

func (b *XDGOpenBridge) acceptLoop(_ context.Context) {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		go b.handleConn(conn)
	}
}

func (b *XDGOpenBridge) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		url := strings.TrimSpace(scanner.Text())
		if url != "" {
			_ = b.opener(url)
		}
	}
}

// Stop tears down the bridge listener.
func (b *XDGOpenBridge) Stop() {
	b.running.Store(false)
	if b.listener != nil {
		_ = b.listener.Close()
	}
}

// Status returns the bridge status.
func (b *XDGOpenBridge) Status() string {
	if b.running.Load() {
		return fmt.Sprintf("xdg-open bridge running (%s)", b.listenAddr)
	}
	return "xdg-open bridge stopped"
}

// openURL opens a URL in the default browser.
func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("xdg-open not supported on %s", runtime.GOOS)
	}
}
