package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	hostName := "test-state-roundtrip"
	state := &TunnelState{
		PID:       os.Getpid(),
		Host:      hostName,
		Hostname:  hostName + ".hop",
		Interface: "utun5",
		StartedAt: time.Now(),
		Connected: true,
	}

	if err := WriteState(state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	t.Cleanup(func() { _ = RemoveState(hostName) })

	loaded, err := LoadState(hostName)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil for alive PID")
	}
	if loaded.PID != state.PID {
		t.Errorf("PID: got %d, want %d", loaded.PID, state.PID)
	}
	if loaded.Hostname != state.Hostname {
		t.Errorf("Hostname: got %q, want %q", loaded.Hostname, state.Hostname)
	}
	if loaded.Interface != state.Interface {
		t.Errorf("Interface: got %q, want %q", loaded.Interface, state.Interface)
	}
}

func TestStateStalePID(t *testing.T) {
	// Start a subprocess and let it exit to get a dead PID.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run 'true': %v", err)
	}
	deadPID := cmd.Process.Pid

	hostName := "test-state-stale"
	state := &TunnelState{
		PID:       deadPID,
		Host:      hostName,
		Hostname:  hostName + ".hop",
		StartedAt: time.Now(),
	}

	if err := WriteState(state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	// LoadState should detect the stale PID and return nil.
	loaded, err := LoadState(hostName)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for stale PID, got %+v", loaded)
	}

	// State file should have been cleaned up.
	dir, dirErr := stateDir()
	if dirErr != nil {
		t.Fatalf("stateDir: %v", dirErr)
	}
	path := filepath.Join(dir, hostName+".json")
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("stale state file was not removed")
		_ = os.Remove(path) // cleanup
	}
}

func TestLoadStateMissing(t *testing.T) {
	state, err := LoadState("definitely-does-not-exist-hopbox-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil, got %+v", state)
	}
}
