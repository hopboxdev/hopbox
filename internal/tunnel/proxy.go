package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
)

// ProxyConfig describes a single port forward.
type ProxyConfig struct {
	LocalAddr  string // "127.0.0.1:4200" (port 0 = ephemeral)
	RemoteAddr string // "10.10.0.2:4200"
	Label      string // "agent-api", "postgres", etc.
}

// Proxy is a running TCP port forwarder.
type Proxy struct {
	listener   net.Listener
	remoteAddr string
	dial       func(ctx context.Context, network, addr string) (net.Conn, error)
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// StartProxy begins listening and forwarding connections.
// Returns the Proxy with the actual bound address (useful when port 0 was requested).
func StartProxy(
	ctx context.Context,
	cfg ProxyConfig,
	dial func(ctx context.Context, network, addr string) (net.Conn, error),
) (*Proxy, error) {
	ln, err := listenWithRetry(cfg.LocalAddr)
	if err != nil {
		return nil, fmt.Errorf("proxy %s: %w", cfg.Label, err)
	}

	proxyCtx, cancel := context.WithCancel(ctx)
	p := &Proxy{
		listener:   ln,
		remoteAddr: cfg.RemoteAddr,
		dial:       dial,
		cancel:     cancel,
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.acceptLoop(proxyCtx)
	}()
	return p, nil
}

// LocalAddr returns the actual bound address.
func (p *Proxy) LocalAddr() net.Addr {
	return p.listener.Addr()
}

// Stop closes the listener and waits for the accept loop to exit.
func (p *Proxy) Stop() {
	p.cancel()
	_ = p.listener.Close()
	p.wg.Wait()
}

func (p *Proxy) acceptLoop(ctx context.Context) {
	for {
		local, err := p.listener.Accept()
		if err != nil {
			return
		}
		go p.forward(ctx, local)
	}
}

func (p *Proxy) forward(ctx context.Context, local net.Conn) {
	defer func() { _ = local.Close() }()

	remote, err := p.dial(ctx, "tcp", p.remoteAddr)
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
}

// listenWithRetry tries cfg.LocalAddr first; on EADDRINUSE it retries
// with port+1 … port+10.
func listenWithRetry(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if !isAddrInUse(err) {
		return nil, err
	}

	host, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, err // return original error
	}
	var port int
	if _, scanErr := fmt.Sscan(portStr, &port); scanErr != nil || port == 0 {
		return nil, err // ephemeral or unparseable — no retry
	}

	for i := 1; i <= 10; i++ {
		ln, tryErr := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port+i))
		if tryErr == nil {
			return ln, nil
		}
		if !isAddrInUse(tryErr) {
			return nil, tryErr
		}
	}
	return nil, fmt.Errorf("all ports %d-%d in use", port, port+10)
}

func isAddrInUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.EADDRINUSE
}
