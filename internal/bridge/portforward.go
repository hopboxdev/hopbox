package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/service"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// excludedPorts are never forwarded (SSH, WireGuard, agent API).
var excludedPorts = map[int]bool{
	22:                  true,
	tunnel.AgentAPIPort: true,
	tunnel.DefaultPort:  true,
}

// portProxy holds the listener and cancel for a single forwarded port.
type portProxy struct {
	listener net.Listener
	cancel   context.CancelFunc
	program  string
}

// Option configures a PortForwarder.
type Option func(*PortForwarder)

// WithInterval sets the poll interval.
func WithInterval(d time.Duration) Option {
	return func(pf *PortForwarder) { pf.interval = d }
}

// WithOnForward sets a callback invoked when a port starts forwarding.
func WithOnForward(fn func(int)) Option {
	return func(pf *PortForwarder) { pf.onForward = fn }
}

// WithOnUnforward sets a callback invoked when a port stops forwarding.
func WithOnUnforward(fn func(int)) Option {
	return func(pf *PortForwarder) { pf.onUnforward = fn }
}

// withListPorts overrides the function used to discover remote ports (for testing).
func withListPorts(fn func() ([]service.ListeningPort, error)) Option {
	return func(pf *PortForwarder) { pf.listPorts = fn }
}

// PortForwarder discovers listening ports on the server via RPC and creates
// local TCP proxies (127.0.0.1:PORT → serverIP:PORT) for each.
type PortForwarder struct {
	hostName    string
	serverIP    string
	interval    time.Duration
	mu          sync.Mutex
	proxies     map[int]*portProxy
	onForward   func(int)
	onUnforward func(int)
	listPorts   func() ([]service.ListeningPort, error)
}

// NewPortForwarder creates a PortForwarder.
func NewPortForwarder(hostName, serverIP string, opts ...Option) *PortForwarder {
	pf := &PortForwarder{
		hostName: hostName,
		serverIP: serverIP,
		interval: 3 * time.Second,
		proxies:  make(map[int]*portProxy),
	}
	for _, o := range opts {
		o(pf)
	}
	if pf.listPorts == nil {
		pf.listPorts = pf.rpcListPorts
	}
	return pf
}

// rpcListPorts fetches listening ports from the agent via RPC.
func (pf *PortForwarder) rpcListPorts() ([]service.ListeningPort, error) {
	result, err := rpcclient.Call(pf.hostName, "ports.list", nil)
	if err != nil {
		return nil, err
	}
	var ports []service.ListeningPort
	if err := json.Unmarshal(result, &ports); err != nil {
		return nil, err
	}
	return ports, nil
}

// Run polls ports.list in a loop and manages proxies. Blocks until ctx is done.
func (pf *PortForwarder) Run(ctx context.Context) error {
	ticker := time.NewTicker(pf.interval)
	defer ticker.Stop()

	// Do an initial poll immediately.
	pf.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			pf.Stop()
			return nil
		case <-ticker.C:
			pf.poll(ctx)
		}
	}
}

// Stop tears down all active proxies.
func (pf *PortForwarder) Stop() {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	for port, pp := range pf.proxies {
		pp.cancel()
		_ = pp.listener.Close()
		delete(pf.proxies, port)
	}
}

// Ports returns the currently forwarded ports, sorted ascending.
func (pf *PortForwarder) Ports() []int {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	ports := make([]int, 0, len(pf.proxies))
	for p := range pf.proxies {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// PortInfo returns the currently forwarded ports with program names, sorted ascending.
func (pf *PortForwarder) PortInfo() []tunnel.ForwardedPort {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	info := make([]tunnel.ForwardedPort, 0, len(pf.proxies))
	for p, pp := range pf.proxies {
		info = append(info, tunnel.ForwardedPort{Port: p, Program: pp.program})
	}
	sort.Slice(info, func(i, j int) bool { return info[i].Port < info[j].Port })
	return info
}

func (pf *PortForwarder) poll(ctx context.Context) {
	ports, err := pf.listPorts()
	if err != nil {
		return
	}

	// Build map of remote port → program name.
	remote := make(map[int]string, len(ports))
	for _, p := range ports {
		if !excludedPorts[p.Port] {
			remote[p.Port] = p.Program
		}
	}

	pf.mu.Lock()
	defer pf.mu.Unlock()

	// Remove proxies for ports no longer listening.
	for port, pp := range pf.proxies {
		if _, ok := remote[port]; !ok {
			pp.cancel()
			_ = pp.listener.Close()
			delete(pf.proxies, port)
			if pf.onUnforward != nil {
				pf.onUnforward(port)
			}
		}
	}

	// Start proxies for new ports, update program names for existing ones.
	for port, prog := range remote {
		if pp, exists := pf.proxies[port]; exists {
			pp.program = prog
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			// Port already in use locally — skip silently.
			continue
		}

		proxyCtx, cancel := context.WithCancel(ctx)
		pp := &portProxy{listener: ln, cancel: cancel, program: prog}
		pf.proxies[port] = pp

		go pf.acceptLoop(proxyCtx, ln, port)

		if pf.onForward != nil {
			pf.onForward(port)
		}
	}
}

func (pf *PortForwarder) acceptLoop(_ context.Context, ln net.Listener, port int) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		go pf.proxyConn(conn, port)
	}
}

func (pf *PortForwarder) proxyConn(local net.Conn, port int) {
	defer func() { _ = local.Close() }()

	remote, err := net.Dial("tcp", net.JoinHostPort(pf.serverIP, fmt.Sprint(port)))
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
