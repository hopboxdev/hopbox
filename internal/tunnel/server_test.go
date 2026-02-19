package tunnel_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

// TestServerTunnelLifecycle tests start/stop of a server tunnel using the
// netstack fallback (works on macOS without root).
func TestServerTunnelLifecycle(t *testing.T) {
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	cfg := tunnel.Config{
		PrivateKey:    kp.PrivateKeyHex(),
		PeerPublicKey: peer.PublicKeyHex(),
		LocalIP:       tunnel.ServerIP + "/24",
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    0,
		MTU:           tunnel.DefaultMTU,
	}

	srv := tunnel.NewServerTunnel(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Start(ctx)
	}()

	// Let it start
	time.Sleep(100 * time.Millisecond)

	s := srv.Status()
	if !s.IsUp {
		t.Error("server tunnel should be up after Start")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Start did not return after ctx cancel")
	}
}

// TestServerTunnelDialContext verifies that the non-Linux server can dial
// within its own netstack network.
func TestServerTunnelDialContext(t *testing.T) {
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	cfg := tunnel.Config{
		PrivateKey:    kp.PrivateKeyHex(),
		PeerPublicKey: peer.PublicKeyHex(),
		LocalIP:       tunnel.ServerIP + "/24",
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    0,
		MTU:           tunnel.DefaultMTU,
	}

	srv := tunnel.NewServerTunnel(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := make(chan struct{})
	go func() {
		close(started)
		_ = srv.Start(ctx)
	}()
	<-started
	time.Sleep(100 * time.Millisecond)

	// DialContext should fail with "tunnel not started" only if Start hasn't
	// been called yet. Since we've started it, it should attempt to connect.
	// The dial will fail (no listener) but NOT with "tunnel not started".
	dialCtx, dialCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer dialCancel()

	conn, err := srv.DialContext(dialCtx, "tcp", net.JoinHostPort(tunnel.ServerIP, "9999"))
	if err == nil {
		_ = conn.Close()
		// Unexpected success — fine, no listener was expected
	}
	// We accept any error except "tunnel not started"
	if err != nil {
		if err.Error() == "tunnel not started" {
			t.Error("DialContext returned 'tunnel not started' but tunnel is running")
		}
	}
}
