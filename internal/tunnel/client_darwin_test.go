//go:build darwin

package tunnel

import "testing"

func TestNewKernelTunnel(t *testing.T) {
	cfg := DefaultClientConfig()
	kt := NewKernelTunnel(cfg)
	if kt == nil {
		t.Fatal("expected non-nil")
	}
	// Ready should not be closed yet.
	select {
	case <-kt.Ready():
		t.Fatal("ready should not be closed before Start")
	default:
	}
}
