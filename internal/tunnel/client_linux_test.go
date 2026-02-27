//go:build linux

package tunnel

import (
	"os"
	"testing"
)

func TestNewKernelTunnel(t *testing.T) {
	cfg := DefaultClientConfig()
	f, err := os.CreateTemp("", "tun-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	kt := NewKernelTunnel(cfg, f, "hopbox0")
	if kt == nil {
		t.Fatal("expected non-nil")
	}
	if kt.InterfaceName() != "hopbox0" {
		t.Fatalf("expected hopbox0, got %s", kt.InterfaceName())
	}
	select {
	case <-kt.Ready():
		t.Fatal("ready should not be closed before Start")
	default:
	}
}
