package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeBackendStartStop(t *testing.T) {
	logDir := t.TempDir()
	b := &NativeBackend{
		Command: "sleep 60",
		LogDir:  logDir,
	}
	ctx := context.Background()
	if err := b.Start(ctx, "test-svc"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	running, err := b.IsRunning("test-svc")
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running {
		t.Error("expected service to be running after Start")
	}

	if err := b.Stop("test-svc"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Give supervisor goroutine time to notice the stop.
	time.Sleep(100 * time.Millisecond)

	running, err = b.IsRunning("test-svc")
	if err != nil {
		t.Fatalf("IsRunning after Stop: %v", err)
	}
	if running {
		t.Error("expected service to not be running after Stop")
	}
}

func TestNativeBackendLogsToFile(t *testing.T) {
	logDir := t.TempDir()
	b := &NativeBackend{
		Command: "echo hello-from-native",
		LogDir:  logDir,
	}
	if err := b.Start(context.Background(), "log-svc"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for the short-lived command to finish.
	time.Sleep(500 * time.Millisecond)

	logPath := filepath.Join(logDir, "log-svc.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got == "" {
		t.Error("expected log file to contain output")
	}
}

func TestNativeBackendAutoRestart(t *testing.T) {
	logDir := t.TempDir()
	b := &NativeBackend{
		Command: "exit 1",
		LogDir:  logDir,
	}
	if err := b.Start(context.Background(), "crash-svc"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for initial run + at least one restart attempt.
	time.Sleep(2 * time.Second)

	b.mu.Lock()
	restarts := b.restartCount
	b.mu.Unlock()

	if restarts < 1 {
		t.Errorf("expected at least 1 restart, got %d", restarts)
	}

	// Clean up.
	if err := b.Stop("crash-svc"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestNativeBackendStopSuppressesRestart(t *testing.T) {
	logDir := t.TempDir()
	b := &NativeBackend{
		Command: "sleep 60",
		LogDir:  logDir,
	}
	if err := b.Start(context.Background(), "stop-svc"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := b.Stop("stop-svc"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	running, _ := b.IsRunning("stop-svc")
	if running {
		t.Error("service should not restart after explicit Stop")
	}
}

func TestNativeBackendWorkdir(t *testing.T) {
	logDir := t.TempDir()
	workDir := t.TempDir()
	b := &NativeBackend{
		Command: "pwd",
		Workdir: workDir,
		LogDir:  logDir,
	}
	if err := b.Start(context.Background(), "pwd-svc"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(filepath.Join(logDir, "pwd-svc.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); !strings.Contains(got, workDir) {
		t.Errorf("expected log to contain workdir %q, got %q", workDir, got)
	}
}

func TestNativeBackendLogCmd(t *testing.T) {
	logDir := t.TempDir()
	b := &NativeBackend{
		Command: "echo test",
		LogDir:  logDir,
	}
	cmd := b.LogCmd("my-svc", 50)
	if cmd == nil {
		t.Fatal("LogCmd returned nil")
	}
	// Verify it's a tail command pointing at the right file.
	args := cmd.Args
	if len(args) < 2 || args[0] != "tail" {
		t.Errorf("expected tail command, got %v", args)
	}
}
