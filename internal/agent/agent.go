package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/hopboxdev/hopbox/internal/service"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// Agent manages the tunnel lifecycle and the control API server.
type Agent struct {
	cfg      tunnel.Config
	tunnel   tunnel.Tunnel
	services *service.Manager
}

// New creates a new Agent with the given tunnel configuration.
func New(cfg tunnel.Config) *Agent {
	return &Agent{cfg: cfg}
}

// WithServices attaches a service manager to the agent.
func (a *Agent) WithServices(sm *service.Manager) {
	a.services = sm
}

// Handler returns the HTTP mux for the agent's control API.
// Useful for testing without starting a real listener or tunnel.
func Handler(a *Agent) http.Handler {
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	return mux
}

// Run starts the WireGuard tunnel and the HTTP control API on the default
// WireGuard IP. Blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	return a.RunOnAddr(ctx, tunnel.ServerIP, tunnel.AgentAPIPort)
}

// RunOnAddr starts the WireGuard tunnel and the HTTP control API on the given
// address. Exposed separately so tests can bind to a free port on 127.0.0.1.
func (a *Agent) RunOnAddr(ctx context.Context, host string, port int) error {
	tun := tunnel.NewServerTunnel(a.cfg)
	a.tunnel = tun

	// Start tunnel in background
	tunnelDone := make(chan error, 1)
	go func() {
		tunnelDone <- tun.Start(ctx)
	}()

	apiAddr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	mux := http.NewServeMux()
	a.registerRoutes(mux)

	srv := &http.Server{
		Addr:    apiAddr,
		Handler: mux,
	}

	listener, err := net.Listen("tcp", apiAddr)
	if err != nil {
		return fmt.Errorf("listen on API address %s: %w", apiAddr, err)
	}

	apiDone := make(chan error, 1)
	go func() {
		apiDone <- srv.Serve(listener)
	}()

	<-ctx.Done()

	_ = srv.Shutdown(context.Background())
	_ = listener.Close()
	tun.Stop()

	<-tunnelDone
	<-apiDone

	return nil
}
