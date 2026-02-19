package tunnel_test

import (
	"strings"
	"testing"

	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

func makeTestConfig(t *testing.T) tunnel.Config {
	t.Helper()
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return tunnel.Config{
		PrivateKey:          kp.PrivateKeyHex(),
		PeerPublicKey:       peer.PublicKeyHex(),
		LocalIP:             tunnel.ClientIP + "/24",
		PeerIP:              tunnel.ServerIP + "/32",
		Endpoint:            "1.2.3.4:51820",
		ListenPort:          0,
		MTU:                 tunnel.DefaultMTU,
		PersistentKeepalive: tunnel.DefaultKeepalive,
	}
}

func TestBuildClientIPC_Format(t *testing.T) {
	cfg := makeTestConfig(t)
	ipc := tunnel.BuildClientIPC(cfg)

	// Must contain private key
	if !strings.Contains(ipc, "private_key="+cfg.PrivateKey) {
		t.Error("missing private_key")
	}
	// Must contain peer public key
	if !strings.Contains(ipc, "public_key="+cfg.PeerPublicKey) {
		t.Error("missing public_key")
	}
	// Must contain endpoint
	if !strings.Contains(ipc, "endpoint="+cfg.Endpoint) {
		t.Error("missing endpoint")
	}
	// Must contain keepalive
	if !strings.Contains(ipc, "persistent_keepalive_interval=25") {
		t.Error("missing persistent_keepalive_interval")
	}
	// Must NOT contain listen_port when 0
	if strings.Contains(ipc, "listen_port=") {
		t.Error("should not contain listen_port when ListenPort=0")
	}
	// Keys must be 64 hex chars
	if len(cfg.PrivateKey) != 64 {
		t.Errorf("private key hex length = %d, want 64", len(cfg.PrivateKey))
	}
	if len(cfg.PeerPublicKey) != 64 {
		t.Errorf("peer public key hex length = %d, want 64", len(cfg.PeerPublicKey))
	}
}

func TestBuildServerIPC_Format(t *testing.T) {
	cfg := makeTestConfig(t)
	cfg.ListenPort = 51820
	ipc := tunnel.BuildServerIPC(cfg)

	if !strings.Contains(ipc, "private_key="+cfg.PrivateKey) {
		t.Error("missing private_key")
	}
	if !strings.Contains(ipc, "listen_port=51820") {
		t.Error("missing listen_port")
	}
	if !strings.Contains(ipc, "public_key="+cfg.PeerPublicKey) {
		t.Error("missing public_key")
	}
	// Server should not have endpoint
	if strings.Contains(ipc, "endpoint=") {
		t.Error("server IPC should not contain endpoint")
	}
}

func TestBuildIPC_AllowedIP(t *testing.T) {
	cfg := makeTestConfig(t)
	clientIPC := tunnel.BuildClientIPC(cfg)
	if !strings.Contains(clientIPC, "allowed_ip="+cfg.PeerIP) {
		t.Errorf("client IPC missing allowed_ip=%s", cfg.PeerIP)
	}
}

func TestBuildClientIPC_NoKeepaliveWhenZero(t *testing.T) {
	cfg := makeTestConfig(t)
	cfg.PersistentKeepalive = 0
	ipc := tunnel.BuildClientIPC(cfg)
	if strings.Contains(ipc, "persistent_keepalive_interval") {
		t.Error("should not contain keepalive when duration is 0")
	}
}
