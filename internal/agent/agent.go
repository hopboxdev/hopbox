package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/service"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// Agent manages the tunnel lifecycle and the control API server.
type Agent struct {
	cfg          tunnel.Config
	tunnel       tunnel.Tunnel
	services     *service.Manager
	scripts      map[string]string
	backupTarget string
	backupPaths  []string
}

// New creates a new Agent with the given tunnel configuration.
func New(cfg tunnel.Config) *Agent {
	return &Agent{cfg: cfg}
}

// WithServices attaches a service manager to the agent.
func (a *Agent) WithServices(sm *service.Manager) {
	a.services = sm
}

// WithScripts attaches a map of named scripts (name → shell command) to the agent.
func (a *Agent) WithScripts(scripts map[string]string) {
	a.scripts = scripts
}

// WithBackupConfig attaches a restic backup target and the list of paths to
// back up. Must be called before Run.
func (a *Agent) WithBackupConfig(target string, paths []string) {
	a.backupTarget = target
	a.backupPaths = paths
}

// Reload re-wires the agent's runtime state (scripts, backup config) from the
// given workspace manifest. Called after workspace.sync is received.
func (a *Agent) Reload(ws *manifest.Workspace) {
	a.scripts = ws.Scripts
	if ws.Backup != nil {
		a.backupTarget = ws.Backup.Target
	} else {
		a.backupTarget = ""
		a.backupPaths = nil
	}
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

	tunnelDone := make(chan error, 1)
	go func() {
		tunnelDone <- tun.Start(ctx)
	}()

	// Wait for the WireGuard interface to be assigned its IP before
	// trying to bind the control API on that address.
	select {
	case <-tun.Ready():
	case err := <-tunnelDone:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	apiAddr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	listener, err := net.Listen("tcp", apiAddr)
	if err != nil {
		tun.Stop()
		<-tunnelDone
		return fmt.Errorf("listen on API address %s: %w", apiAddr, err)
	}

	_ = a.serveHTTP(ctx, listener)
	tun.Stop()
	<-tunnelDone
	return nil
}

// RunOnListener starts only the HTTP control API on the provided listener,
// without managing a WireGuard tunnel. The caller is responsible for the
// tunnel; this is the entry point used by end-to-end tests.
func (a *Agent) RunOnListener(ctx context.Context, listener net.Listener) error {
	return a.serveHTTP(ctx, listener)
}

// serveHTTP starts the HTTP server on listener and blocks until ctx is
// cancelled, then shuts down gracefully.
func (a *Agent) serveHTTP(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	srv := &http.Server{Handler: mux}

	done := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()

	<-ctx.Done()
	_ = srv.Shutdown(context.Background())
	_ = listener.Close()
	return <-done
}
