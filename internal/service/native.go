package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	stopTimeout     = 5 * time.Second
	maxBackoff      = 30 * time.Second
	backoffResetAge = 60 * time.Second
)

// NativeBackend manages a service as a host process.
type NativeBackend struct {
	Command string
	Workdir string
	Env     map[string]string
	LogDir  string

	mu           sync.Mutex
	cmd          *exec.Cmd
	stopped      bool // true when Stop is called explicitly
	cancel       context.CancelFunc
	restartCount int
}

// Start launches the process and begins supervising it.
func (n *NativeBackend) Start(_ context.Context, name string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stopped = false
	n.restartCount = 0

	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel

	if err := n.startProcess(name); err != nil {
		cancel()
		return err
	}

	go n.supervise(ctx, name)
	return nil
}

func (n *NativeBackend) startProcess(name string) error {
	if err := os.MkdirAll(n.LogDir, 0700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(n.LogDir, name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	cmd := exec.Command("sh", "-c", n.Command)
	cmd.Dir = n.Workdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Merge environment.
	cmd.Env = os.Environ()
	for k, v := range n.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start %q: %w", name, err)
	}

	// Close log file when process exits.
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	n.cmd = cmd
	return nil
}

func (n *NativeBackend) supervise(ctx context.Context, name string) {
	backoff := time.Second
	for {
		// Wait for process to exit.
		n.mu.Lock()
		cmd := n.cmd
		n.mu.Unlock()
		if cmd == nil || cmd.Process == nil {
			return
		}

		startedAt := time.Now()
		// Block until process exits (Wait already called in startProcess goroutine,
		// so we poll IsRunning instead).
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
				running, _ := n.isAlive()
				if !running {
					goto exited
				}
			}
		}
	exited:

		n.mu.Lock()
		if n.stopped {
			n.mu.Unlock()
			return
		}
		n.restartCount++
		n.mu.Unlock()

		// Reset backoff if process ran long enough.
		if time.Since(startedAt) >= backoffResetAge {
			backoff = time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		n.mu.Lock()
		if n.stopped {
			n.mu.Unlock()
			return
		}
		err := n.startProcess(name)
		n.mu.Unlock()
		if err != nil {
			return
		}

		// Increase backoff.
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// Stop sends SIGTERM to the process group, then SIGKILL after a timeout.
func (n *NativeBackend) Stop(_ string) error {
	n.mu.Lock()
	n.stopped = true
	cmd := n.cmd
	cancel := n.cancel
	n.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Send SIGTERM to process group.
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)

	// Wait for exit or timeout.
	done := make(chan struct{})
	go func() {
		for {
			if alive, _ := n.isAlive(); !alive {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-done:
		return nil
	case <-time.After(stopTimeout):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
		return nil
	}
}

// IsRunning reports whether the managed process is alive.
func (n *NativeBackend) IsRunning(_ string) (bool, error) {
	return n.isAlive()
}

func (n *NativeBackend) isAlive() (bool, error) {
	n.mu.Lock()
	cmd := n.cmd
	n.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return false, nil
	}
	err := syscall.Kill(cmd.Process.Pid, 0)
	return err == nil, nil
}

// LogCmd returns a tail command for streaming the service log file.
func (n *NativeBackend) LogCmd(name string, tail int) *exec.Cmd {
	logPath := filepath.Join(n.LogDir, name+".log")
	return exec.Command("tail", "-n", fmt.Sprintf("%d", tail), "-f", logPath)
}
