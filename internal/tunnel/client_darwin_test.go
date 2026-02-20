//go:build darwin

package tunnel

import (
	"os"
	"testing"
)

func TestNewKernelTunnel(t *testing.T) {
	cfg := DefaultClientConfig()
	// Pass a dummy file and name — we're only testing the constructor, not Start.
	f, err := os.CreateTemp("", "utun-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	kt := NewKernelTunnel(cfg, f, "utun99")
	if kt == nil {
		t.Fatal("expected non-nil")
	}
	if kt.InterfaceName() != "utun99" {
		t.Fatalf("expected utun99, got %s", kt.InterfaceName())
	}
	// Ready should not be closed yet.
	select {
	case <-kt.Ready():
		t.Fatal("ready should not be closed before Start")
	default:
	}
}
