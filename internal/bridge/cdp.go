package bridge

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
)

const defaultCDPPort = 9222

// CDPBridge forwards Chrome DevTools Protocol connections between the remote
// agent and the local Chrome instance.
type CDPBridge struct {
	listenAddr string
	targetPort int
	listener   net.Listener
	running    atomic.Bool
}

// NewCDPBridge creates a CDP bridge using the default CDP port (9222).
func NewCDPBridge(serverIP string) *CDPBridge {
	return NewCDPBridgeOnPort(serverIP, defaultCDPPort, defaultCDPPort)
}

// NewCDPBridgeOnPort creates a CDP bridge with configurable listen and target ports.
// If listenPort is 0, an ephemeral port is used.
func NewCDPBridgeOnPort(listenIP string, listenPort, targetPort int) *CDPBridge {
	return &CDPBridge{
		listenAddr: fmt.Sprintf("%s:%d", listenIP, listenPort),
		targetPort: targetPort,
	}
}

// Start begins listening for CDP proxy connections.
func (b *CDPBridge) Start(ctx context.Context) error {
	return b.start(ctx, nil)
}

// StartWithNotify starts the bridge and sends the bound port to ch once listening.
func (b *CDPBridge) StartWithNotify(ctx context.Context, ch chan<- int) error {
	return b.start(ctx, ch)
}

func (b *CDPBridge) start(ctx context.Context, portNotify chan<- int) error {
	ln, err := net.Listen("tcp", b.listenAddr)
	if err != nil {
		return fmt.Errorf("CDP bridge listen: %w", err)
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

func (b *CDPBridge) acceptLoop(ctx context.Context) {
	for {
		remote, err := b.listener.Accept()
		if err != nil {
			return
		}
		go b.proxy(ctx, remote)
	}
}

func (b *CDPBridge) proxy(_ context.Context, remote net.Conn) {
	defer remote.Close()

	local, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", b.targetPort))
	if err != nil {
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = copyConn(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = copyConn(local, remote)
		done <- struct{}{}
	}()
	<-done
}

// Stop tears down the bridge listener.
func (b *CDPBridge) Stop() {
	b.running.Store(false)
	if b.listener != nil {
		_ = b.listener.Close()
	}
}

// Status returns the bridge status.
func (b *CDPBridge) Status() string {
	if b.running.Load() {
		return fmt.Sprintf("CDP bridge running (%s → 127.0.0.1:%d)", b.listenAddr, b.targetPort)
	}
	return "CDP bridge stopped"
}

func copyConn(dst, src net.Conn) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			written, werr := dst.Write(buf[:n])
			total += int64(written)
			if werr != nil {
				return total, werr
			}
		}
		if err != nil {
			return total, err
		}
	}
}
