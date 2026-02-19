package bridge

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"sync/atomic"
)

const defaultClipboardPort = 2224

// ClipboardBridge implements a bidirectional clipboard bridge over TCP.
// On the client (macOS/Linux), it listens for clipboard content from the
// server and syncs to the local clipboard via pbcopy/xclip.
type ClipboardBridge struct {
	serverAddr      string
	listenAddr      string
	listener        net.Listener
	running         atomic.Bool
	clipboardWriter func([]byte) error // injectable for tests; defaults to copyToClipboard
}

// NewClipboardBridge creates a clipboard bridge that listens on the default port.
func NewClipboardBridge(serverIP string) *ClipboardBridge {
	return NewClipboardBridgeOnPort(serverIP, defaultClipboardPort)
}

// NewClipboardBridgeOnPort creates a clipboard bridge on a specific port.
// If port is 0, an ephemeral port is used.
func NewClipboardBridgeOnPort(listenIP string, port int) *ClipboardBridge {
	return &ClipboardBridge{
		serverAddr:      fmt.Sprintf("%s:%d", listenIP, port),
		listenAddr:      fmt.Sprintf("%s:%d", listenIP, port),
		clipboardWriter: copyToClipboard,
	}
}

// SetClipboardWriter overrides the clipboard write function. Used in tests.
func (b *ClipboardBridge) SetClipboardWriter(fn func([]byte) error) {
	b.clipboardWriter = fn
}

// Start begins listening for clipboard events.
func (b *ClipboardBridge) Start(ctx context.Context) error {
	return b.start(ctx, nil)
}

// StartWithNotify starts the bridge and sends the bound port to ch once listening.
func (b *ClipboardBridge) StartWithNotify(ctx context.Context, ch chan<- int) error {
	return b.start(ctx, ch)
}

func (b *ClipboardBridge) start(ctx context.Context, portNotify chan<- int) error {
	ln, err := net.Listen("tcp", b.listenAddr)
	if err != nil {
		return fmt.Errorf("clipboard bridge listen: %w", err)
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

func (b *ClipboardBridge) acceptLoop(ctx context.Context) {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		go b.handleConn(ctx, conn)
	}
}

func (b *ClipboardBridge) handleConn(_ context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	data, err := io.ReadAll(io.LimitReader(conn, 1<<20)) // 1 MB limit
	if err != nil || len(data) == 0 {
		return
	}
	_ = b.clipboardWriter(data)
}

// Stop tears down the bridge listener.
func (b *ClipboardBridge) Stop() {
	b.running.Store(false)
	if b.listener != nil {
		_ = b.listener.Close()
	}
}

// Status returns the bridge status.
func (b *ClipboardBridge) Status() string {
	if b.running.Load() {
		return fmt.Sprintf("clipboard bridge running (%s)", b.listenAddr)
	}
	return "clipboard bridge stopped"
}

// copyToClipboard writes data to the system clipboard.
func copyToClipboard(data []byte) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}

	pipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, _ = pipe.Write(data)
	_ = pipe.Close()
	return cmd.Wait()
}

// ReadClipboard reads the current clipboard content.
func ReadClipboard() ([]byte, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard", "-out")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--output")
		}
	default:
		return nil, fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	return cmd.Output()
}
