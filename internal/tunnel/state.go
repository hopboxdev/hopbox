package tunnel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// TunnelState describes a running hop up process.
// Written to ~/.config/hopbox/run/<host>.json.
type TunnelState struct {
	PID          int               `json:"pid"`
	Host         string            `json:"host"`
	AgentAPIAddr string            `json:"agent_api_addr"`          // "127.0.0.1:4200"
	SSHAddr      string            `json:"ssh_addr,omitempty"`      // "127.0.0.1:2222"
	ServicePorts map[string]string `json:"service_ports,omitempty"` // "postgres:5432" → "127.0.0.1:5432"
	StartedAt    time.Time         `json:"started_at"`
	Connected    bool              `json:"connected"`
	LastHealthy  time.Time         `json:"last_healthy,omitempty"`
}

// stateDir returns ~/.config/hopbox/run.
func stateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopbox", "run"), nil
}

// WriteState writes the tunnel state to disk.
func WriteState(state *TunnelState) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, state.Host+".json"), data, 0600)
}

// LoadState reads the tunnel state for hostName.
// Returns nil, nil if the file does not exist or the recorded process is no longer running.
func LoadState(hostName string) (*TunnelState, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, hostName+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state %q: %w", hostName, err)
	}
	var state TunnelState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state %q: %w", hostName, err)
	}
	if state.PID > 0 && !pidAlive(state.PID) {
		_ = os.Remove(path)
		return nil, nil
	}
	return &state, nil
}

// RemoveState deletes the tunnel state file for hostName.
func RemoveState(hostName string) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, hostName+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state %q: %w", hostName, err)
	}
	return nil
}

// pidAlive reports whether a process with the given PID is running.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
