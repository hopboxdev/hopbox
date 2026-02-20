package tunnel_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

// TestLoopbackWireGuard runs two netstack WireGuard devices in one process,
// talking over in-memory channel binds (no real network, no root required).
// A tiny echo server listens on the "server" netstack; the "client" dials
// through WireGuard and verifies the response.
//
// The bindtest.ChannelBind has pre-wired endpoints:
//
//	binds[0].target4 = ChannelEndpoint(1)  → client sends to "127.0.0.1:1"
//	binds[1].rx4 receives whatever binds[0] sends to target4
//	binds[1].target6 = ChannelEndpoint(4)  → server replies via target6
//	binds[0].rx6 receives whatever binds[1] sends to target6
func TestLoopbackWireGuard(t *testing.T) {
	// Generate fresh key pairs for both sides.
	clientKP, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	serverKP, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	// WireGuard IPs
	clientAddrStr := "10.10.0.1"
	serverAddrStr := "10.10.0.2"
	clientAddr := netip.MustParseAddr(clientAddrStr)
	serverAddr := netip.MustParseAddr(serverAddrStr)

	// Create in-process channel binds (no real UDP sockets needed).
	// binds[0] = client, binds[1] = server.
	// binds[0].target4 = ChannelEndpoint(1): client sends to "127.0.0.1:1"
	// → lands in binds[1].rx4. So client endpoint = "127.0.0.1:1".
	binds := bindtest.NewChannelBinds()
	clientBind := binds[0]
	serverBind := binds[1]

	mtu := tunnel.DefaultMTU

	// ---- Server netstack ----
	serverTunDev, serverNet, err := netstack.CreateNetTUN(
		[]netip.Addr{serverAddr},
		nil,
		mtu,
	)
	if err != nil {
		t.Fatalf("server CreateNetTUN: %v", err)
	}

	logger := device.NewLogger(device.LogLevelSilent, "")
	serverDev := device.NewDevice(serverTunDev, serverBind, logger)

	serverIPC := fmt.Sprintf(
		"private_key=%s\nlisten_port=51820\npublic_key=%s\nallowed_ip=%s/32\n",
		serverKP.PrivateKeyHex(),
		clientKP.PublicKeyHex(),
		clientAddrStr,
	)
	if err := serverDev.IpcSet(serverIPC); err != nil {
		t.Fatalf("server IpcSet: %v", err)
	}
	if err := serverDev.Up(); err != nil {
		t.Fatalf("server Up: %v", err)
	}
	defer serverDev.Close()

	// ---- Client netstack ----
	// Endpoint = "127.0.0.1:1" → ChannelEndpoint(1) = binds[0].target4
	// Packets sent to this endpoint land in binds[1].rx4 (server receives them).
	clientTunDev, clientNet, err := netstack.CreateNetTUN(
		[]netip.Addr{clientAddr},
		nil,
		mtu,
	)
	if err != nil {
		t.Fatalf("client CreateNetTUN: %v", err)
	}

	clientDev := device.NewDevice(clientTunDev, clientBind, logger)
	clientIPC := fmt.Sprintf(
		"private_key=%s\npublic_key=%s\nallowed_ip=%s/32\nendpoint=127.0.0.1:1\npersistent_keepalive_interval=1\n",
		clientKP.PrivateKeyHex(),
		serverKP.PublicKeyHex(),
		serverAddrStr,
	)
	if err := clientDev.IpcSet(clientIPC); err != nil {
		t.Fatalf("client IpcSet: %v", err)
	}
	if err := clientDev.Up(); err != nil {
		t.Fatalf("client Up: %v", err)
	}
	defer clientDev.Close()

	// ---- Start echo server on server netstack ----
	serverListener, err := serverNet.ListenTCP(&net.TCPAddr{IP: net.ParseIP(serverAddrStr), Port: 9999})
	if err != nil {
		t.Fatalf("server ListenTCP: %v", err)
	}
	defer func() { _ = serverListener.Close() }()

	go func() {
		for {
			c, err := serverListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 256)
				n, _ := c.Read(buf)
				_, _ = c.Write(buf[:n])
			}(c)
		}
	}()

	// ---- Client dials and sends message ----
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var dialConn net.Conn
	var dialErr error
	for i := 0; i < 30; i++ {
		dialConn, dialErr = clientNet.DialContext(ctx, "tcp", serverAddrStr+":9999")
		if dialErr == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if dialErr != nil {
		t.Fatalf("client dial after retries: %v", dialErr)
	}
	defer func() { _ = dialConn.Close() }()

	msg := "hello wireguard"
	if _, err := fmt.Fprint(dialConn, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	// gonet.TCPConn implements CloseWrite but not as *net.TCPConn.
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := dialConn.(closeWriter); ok {
		if err := cw.CloseWrite(); err != nil {
			t.Fatalf("CloseWrite: %v", err)
		}
	} else {
		// Fall back to setting a deadline so ReadAll doesn't block forever.
		_ = dialConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	}

	got, err := io.ReadAll(dialConn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != msg {
		t.Errorf("echo = %q, want %q", got, msg)
	}
}

func TestClientTunnelReady(t *testing.T) {
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	peerKP, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	cfg := tunnel.Config{
		PrivateKey:          kp.PrivateKeyHex(),
		PeerPublicKey:       peerKP.PublicKeyHex(),
		LocalIP:             "10.99.0.1/24",
		PeerIP:              "10.99.0.2/32",
		Endpoint:            "127.0.0.1:51820", // unreachable — that's fine
		ListenPort:          0,
		MTU:                 tunnel.DefaultMTU,
		PersistentKeepalive: 0,
	}
	tun := tunnel.NewClientTunnel(cfg)

	// Ready channel must not be closed before Start is called.
	select {
	case <-tun.Ready():
		t.Fatal("Ready() closed before Start was called")
	default:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = tun.Start(ctx) }()

	select {
	case <-tun.Ready():
		// success — t.tnet is now safely assigned
	case <-time.After(3 * time.Second):
		t.Fatal("Ready() was not closed within 3s of Start")
	}
}
