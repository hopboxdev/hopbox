package hostconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// overrideConfigDir sets XDG_CONFIG_HOME to a temp dir for testing.
func withTempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
}

func makeConfig(name string) *hostconfig.HostConfig {
	return &hostconfig.HostConfig{
		Name:          name,
		Endpoint:      "1.2.3.4:51820",
		PrivateKey:    "privkey==",
		PeerPublicKey: "pubkey==",
		TunnelIP:      "10.10.0.1/24",
		AgentIP:       "10.10.0.2",
		SSHUser:       "root",
		SSHHost:       "1.2.3.4",
		SSHPort:       22,
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	cfg := makeConfig("mybox")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := hostconfig.Load("mybox")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Name != cfg.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, cfg.Name)
	}
	if loaded.Endpoint != cfg.Endpoint {
		t.Errorf("Endpoint = %q, want %q", loaded.Endpoint, cfg.Endpoint)
	}
	if loaded.PrivateKey != cfg.PrivateKey {
		t.Errorf("PrivateKey mismatch")
	}
	if loaded.SSHPort != cfg.SSHPort {
		t.Errorf("SSHPort = %d, want %d", loaded.SSHPort, cfg.SSHPort)
	}
}

func TestList(t *testing.T) {
	withTempConfigDir(t)

	for _, name := range []string{"box1", "box2", "box3"} {
		if err := makeConfig(name).Save(); err != nil {
			t.Fatalf("Save %q: %v", name, err)
		}
	}

	names, err := hostconfig.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 3 {
		t.Errorf("List returned %d items, want 3", len(names))
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, want := range []string{"box1", "box2", "box3"} {
		if !found[want] {
			t.Errorf("List missing %q", want)
		}
	}
}

func TestListEmpty(t *testing.T) {
	withTempConfigDir(t)
	names, err := hostconfig.List()
	if err != nil {
		t.Fatalf("List on empty dir: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestDelete(t *testing.T) {
	withTempConfigDir(t)

	cfg := makeConfig("deleteme")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := hostconfig.Delete("deleteme"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	names, _ := hostconfig.List()
	for _, n := range names {
		if n == "deleteme" {
			t.Error("deleted config still appears in List")
		}
	}
}

func TestLoadNotExist(t *testing.T) {
	withTempConfigDir(t)
	_, err := hostconfig.Load("doesnotexist")
	if err == nil {
		t.Error("expected error loading non-existent config")
	}
}

func TestSaveFilePermissions(t *testing.T) {
	withTempConfigDir(t)

	cfg := makeConfig("permtest")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "hopbox", "hosts", "permtest.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestToTunnelConfig(t *testing.T) {
	cfg := makeConfig("tunneltest")
	tc := cfg.ToTunnelConfig()

	if tc.PrivateKey != cfg.PrivateKey {
		t.Errorf("PrivateKey mismatch")
	}
	if tc.PeerPublicKey != cfg.PeerPublicKey {
		t.Errorf("PeerPublicKey mismatch")
	}
	if tc.Endpoint != cfg.Endpoint {
		t.Errorf("Endpoint mismatch")
	}
	if tc.LocalIP != cfg.TunnelIP {
		t.Errorf("LocalIP = %q, want %q", tc.LocalIP, cfg.TunnelIP)
	}
}
