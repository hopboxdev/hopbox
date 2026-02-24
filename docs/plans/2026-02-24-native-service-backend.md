# Native Service Backend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run processes directly on the host without Docker, managed by the same service orchestration system.

**Architecture:** Implement `NativeBackend` behind the existing `Backend` interface. A supervisor goroutine handles auto-restart with exponential backoff. Logs go to disk files. The `Backend` interface gains a `LogCmd` method so log streaming works for both Docker and native services.

**Tech Stack:** Go stdlib (`os/exec`, `syscall`), existing `internal/service` package.

---

### Task 1: Add Workdir to manifest and validate native requires command

**Files:**
- Modify: `internal/manifest/manifest.go:33-42` (Service struct)
- Modify: `internal/manifest/manifest.go:110-127` (Validate)
- Modify: `internal/manifest/manifest_test.go`

**Step 1: Write the failing test**

Add to `internal/manifest/manifest_test.go`:

```go
func TestValidateNativeMissingCommand(t *testing.T) {
	_, err := manifest.ParseBytes([]byte(`
name: test
services:
  svc:
    type: native
`))
	if err == nil {
		t.Error("expected error for native service without command")
	}
}

func TestParseWorkdir(t *testing.T) {
	ws, err := manifest.ParseBytes([]byte(`
name: test
services:
  api:
    type: native
    command: ./server
    workdir: /home/user/app
`))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if ws.Services["api"].Workdir != "/home/user/app" {
		t.Errorf("Workdir = %q, want /home/user/app", ws.Services["api"].Workdir)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/manifest/... -run "TestValidateNativeMissingCommand|TestParseWorkdir" -v`
Expected: `TestParseWorkdir` fails (Workdir field doesn't exist), `TestValidateNativeMissingCommand` fails (no validation).

**Step 3: Implement**

In `internal/manifest/manifest.go`, add `Workdir` to Service struct:

```go
type Service struct {
	Type      string            `yaml:"type"`
	Image     string            `yaml:"image,omitempty"`
	Command   string            `yaml:"command,omitempty"`
	Workdir   string            `yaml:"workdir,omitempty"`
	Ports     []string          `yaml:"ports,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Health    *HealthCheck      `yaml:"health,omitempty"`
	Data      []DataMount       `yaml:"data,omitempty"`
	DependsOn []string          `yaml:"depends_on,omitempty"`
}
```

In `Validate()`, after the type switch, add native command validation:

```go
if svc.Type == "native" && svc.Command == "" {
	return fmt.Errorf("service %q: command is required for native services", name)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/manifest/... -v`
Expected: All pass.

**Step 5: Commit**

```
git add internal/manifest/manifest.go internal/manifest/manifest_test.go
git commit -m "feat: add workdir field and validate native requires command"
```

---

### Task 2: Add LogCmd to Backend interface and implement for Docker

**Files:**
- Modify: `internal/service/manager.go:27-32` (Backend interface)
- Modify: `internal/service/docker.go` (add LogCmd method)
- Modify: `internal/service/manager.go` (add Backend accessor)
- Modify: `internal/service/manager_test.go` (update stubs)

**Step 1: Write the failing test**

Add to `internal/service/manager_test.go`:

```go
func TestManagerBackendAccessor(t *testing.T) {
	m := service.NewManager()
	b := &stubBackend{}
	m.Register(&service.Def{Name: "app", Type: "native"}, b)

	got := m.Backend("app")
	if got != b {
		t.Error("Backend accessor should return registered backend")
	}
	if m.Backend("nonexistent") != nil {
		t.Error("Backend accessor should return nil for unknown service")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/service/... -run TestManagerBackendAccessor -v`
Expected: FAIL — `Backend` method doesn't exist.

**Step 3: Implement**

In `internal/service/manager.go`, add `LogCmd` to the Backend interface:

```go
type Backend interface {
	Start(ctx context.Context, name string) error
	Stop(name string) error
	IsRunning(name string) (bool, error)
	LogCmd(name string, tail int) *exec.Cmd
}
```

Add the import `"os/exec"` to manager.go.

Add `Backend` accessor to Manager:

```go
// Backend returns the backend for the named service, or nil if not found.
func (m *Manager) Backend(name string) Backend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backends[name]
}
```

In `internal/service/docker.go`, add LogCmd:

```go
// LogCmd returns a command that streams container logs.
func (d *DockerBackend) LogCmd(name string, tail int) *exec.Cmd {
	return exec.Command("docker", "logs", "--follow", "--tail", fmt.Sprintf("%d", tail), "--", name)
}
```

Add `"fmt"` to docker.go imports.

Update stubs in `internal/service/manager_test.go` to satisfy the interface:

```go
func (s *stubBackend) LogCmd(_ string, _ int) *exec.Cmd { return nil }
```

And for errorBackend:

```go
func (e *errorBackend) LogCmd(_ string, _ int) *exec.Cmd { return nil }
```

Add `"os/exec"` to manager_test.go imports.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/... -v`
Expected: All pass.

**Step 5: Commit**

```
git add internal/service/manager.go internal/service/docker.go internal/service/manager_test.go
git commit -m "feat: add LogCmd to Backend interface with Docker implementation"
```

---

### Task 3: Add StopAll with reverse dependency order

**Files:**
- Modify: `internal/service/manager.go` (add StopAll method)
- Modify: `internal/service/manager_test.go` (test reverse order)

**Step 1: Write the failing test**

Add to `internal/service/manager_test.go`:

```go
func TestManagerStopAllReverseDependencyOrder(t *testing.T) {
	m := service.NewManager()
	var stopOrder []string

	for _, name := range []string{"db", "cache", "api"} {
		n := name
		b := &stubBackend{
			running: true,
			stopFn: func() error {
				stopOrder = append(stopOrder, n)
				return nil
			},
		}
		var deps []string
		if n == "api" {
			deps = []string{"db", "cache"}
		}
		m.Register(&service.Def{Name: n, Type: "docker", DependsOn: deps}, b)
	}

	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	// api must stop before both db and cache
	apiIdx := -1
	for i, name := range stopOrder {
		if name == "api" {
			apiIdx = i
		}
	}
	if apiIdx == -1 {
		t.Fatal("api was not stopped")
	}
	for _, dep := range []string{"db", "cache"} {
		depIdx := -1
		for i, name := range stopOrder {
			if name == dep {
				depIdx = i
			}
		}
		if depIdx <= apiIdx {
			t.Errorf("expected %s to stop after api (stop order: %v)", dep, stopOrder)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/service/... -run TestManagerStopAllReverseDependencyOrder -v`
Expected: FAIL — `StopAll` method doesn't exist.

**Step 3: Implement**

Add to `internal/service/manager.go`:

```go
// StopAll stops all registered services in reverse dependency order.
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defs := make(map[string]*Def, len(m.services))
	for k, v := range m.services {
		defs[k] = v
	}
	m.mu.Unlock()

	order, err := topoSort(defs)
	if err != nil {
		return fmt.Errorf("service ordering: %w", err)
	}

	// Reverse: stop dependents before their dependencies.
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}

	var firstErr error
	for _, name := range order {
		if err := m.Stop(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/... -v`
Expected: All pass.

**Step 5: Commit**

```
git add internal/service/manager.go internal/service/manager_test.go
git commit -m "feat: add StopAll with reverse dependency ordering"
```

---

### Task 4: Implement NativeBackend

**Files:**
- Create: `internal/service/native.go`
- Create: `internal/service/native_test.go`

**Step 1: Write the failing tests**

Create `internal/service/native_test.go`:

```go
package service

import (
	"context"
	"os"
	"path/filepath"
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
	if got := string(data); !contains(got, workDir) {
		t.Errorf("expected log to contain workdir %q, got %q", workDir, got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/... -run "TestNativeBackend" -v`
Expected: FAIL — `NativeBackend` type doesn't exist.

**Step 3: Implement**

Create `internal/service/native.go`:

```go
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/... -run "TestNativeBackend" -v -count=1`
Expected: All pass.

**Step 5: Run the full service test suite**

Run: `go test ./internal/service/... -v`
Expected: All pass (including existing Docker/Manager tests).

**Step 6: Commit**

```
git add internal/service/native.go internal/service/native_test.go
git commit -m "feat: implement NativeBackend with auto-restart and log files"
```

---

### Task 5: Wire NativeBackend into BuildServiceManager

**Files:**
- Modify: `internal/service/manager.go:34-45` (add Workdir to Def)
- Modify: `internal/agent/services.go:15-83` (create NativeBackend for native type)

**Step 1: Add Workdir to Def**

In `internal/service/manager.go`, add `Workdir` field to Def:

```go
type Def struct {
	Name      string
	Type      string // "docker", "native"
	Image     string // for docker
	Command   string // for native
	Workdir   string // for native
	Ports     []string
	Env       map[string]string
	DependsOn []string
	Health    *HealthCheck
	DataPaths []string
}
```

**Step 2: Wire NativeBackend in BuildServiceManager**

In `internal/agent/services.go`, add the native case alongside the docker case. Add imports for `"os"`, `"path/filepath"`, and the log directory constant:

```go
func BuildServiceManager(ws *manifest.Workspace) *service.Manager {
	mgr := service.NewManager()
	for name, svc := range ws.Services {
		var dataPaths []string
		var volumes []string
		for _, d := range svc.Data {
			if d.Host != "" {
				dataPaths = append(dataPaths, d.Host)
			}
			if d.Host != "" && d.Container != "" {
				volumes = append(volumes, d.Host+":"+d.Container)
			}
		}

		var hc *service.HealthCheck
		if svc.Health != nil && svc.Health.HTTP != "" {
			hc = &service.HealthCheck{HTTP: svc.Health.HTTP}
			if svc.Health.Interval != "" {
				hc.Interval, _ = time.ParseDuration(svc.Health.Interval)
			}
			if svc.Health.Timeout != "" {
				hc.Timeout, _ = time.ParseDuration(svc.Health.Timeout)
			}
		}

		def := &service.Def{
			Name:      name,
			Type:      svc.Type,
			Image:     svc.Image,
			Command:   svc.Command,
			Workdir:   svc.Workdir,
			Ports:     svc.Ports,
			Env:       svc.Env,
			DependsOn: svc.DependsOn,
			Health:    hc,
			DataPaths: dataPaths,
		}

		var backend service.Backend
		switch svc.Type {
		case "docker":
			ports := make([]string, 0, len(svc.Ports))
			for _, p := range svc.Ports {
				if strings.ContainsRune(p, ':') {
					ports = append(ports, p)
				} else {
					ports = append(ports, p+":"+p)
				}
			}
			for i, p := range ports {
				if strings.Count(p, ":") < 2 {
					ports[i] = tunnel.ServerIP + ":" + p
				}
			}
			backend = &service.DockerBackend{
				Image:   svc.Image,
				Cmd:     strings.Fields(svc.Command),
				Env:     svc.Env,
				Ports:   ports,
				Volumes: volumes,
			}
		case "native":
			logDir, _ := os.UserConfigDir()
			logDir = filepath.Join(logDir, "hopbox", "logs")
			backend = &service.NativeBackend{
				Command: svc.Command,
				Workdir: svc.Workdir,
				Env:     svc.Env,
				LogDir:  logDir,
			}
		}
		if backend != nil {
			mgr.Register(def, backend)
		}
	}
	return mgr
}
```

**Step 3: Run tests to verify nothing breaks**

Run: `go test ./internal/agent/... ./internal/service/... -v`
Expected: All pass.

**Step 4: Commit**

```
git add internal/service/manager.go internal/agent/services.go
git commit -m "feat: wire NativeBackend into BuildServiceManager"
```

---

### Task 6: Refactor rpcLogsStream to use Backend.LogCmd

**Files:**
- Modify: `internal/agent/api.go:351-449` (rpcLogsStream, streamSingleLog, streamAllServiceLogs)

**Step 1: Refactor streamSingleLog**

Replace the hardcoded Docker command with backend dispatch:

```go
func (a *Agent) streamSingleLog(w http.ResponseWriter, r *http.Request, name string) {
	if a.services == nil {
		writeRPCError(w, http.StatusServiceUnavailable, "service manager not initialised")
		return
	}
	backend := a.services.Backend(name)
	if backend == nil {
		writeRPCError(w, http.StatusBadRequest, fmt.Sprintf("unknown service %q", name))
		return
	}

	cmd := backend.LogCmd(name, 100)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		writeRPCError(w, http.StatusInternalServerError, fmt.Sprintf("log stream: %v", err))
		return
	}
	go func() {
		_ = cmd.Wait()
		_ = pw.Close()
	}()

	// Cancel the command when the client disconnects.
	go func() {
		<-r.Context().Done()
		_ = cmd.Process.Kill()
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	fw := &flushWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		fw.f = f
	}
	_, _ = io.Copy(fw, pr)
}
```

**Step 2: Refactor streamAllServiceLogs**

Remove the Docker-only filter — stream all registered services:

```go
func (a *Agent) streamAllServiceLogs(w http.ResponseWriter, r *http.Request) {
	if a.services == nil {
		writeRPCError(w, http.StatusServiceUnavailable, "service manager not initialised")
		return
	}
	statuses := a.services.ListStatus()
	if len(statuses) == 0 {
		writeRPCError(w, http.StatusBadRequest, "no services registered")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	fw := &flushWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		fw.f = f
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range statuses {
		backend := a.services.Backend(s.Name)
		if backend == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := backend.LogCmd(s.Name, 50)
			pr, pw := io.Pipe()
			cmd.Stdout = pw
			cmd.Stderr = pw
			if err := cmd.Start(); err != nil {
				_ = pw.Close()
				return
			}
			go func() {
				_ = cmd.Wait()
				_ = pw.Close()
			}()
			go func() {
				<-r.Context().Done()
				_ = cmd.Process.Kill()
			}()
			scanner := bufio.NewScanner(pr)
			for scanner.Scan() {
				line := fmt.Sprintf("[%s] %s\n", s.Name, scanner.Text())
				mu.Lock()
				_, _ = fw.Write([]byte(line))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
```

Remove the `isValidContainerName` function if it's no longer used elsewhere.

**Step 3: Run tests**

Run: `go test ./internal/agent/... -v`
Expected: All pass.

Run: `go build ./cmd/hop-agent/...`
Expected: Builds cleanly.

**Step 4: Commit**

```
git add internal/agent/api.go
git commit -m "refactor: use Backend.LogCmd for log streaming instead of hardcoded Docker"
```

---

### Task 7: Full build and test verification

**Step 1: Run the full test suite**

Run: `go test ./... -count=1`
Expected: All packages pass.

**Step 2: Build all binaries**

Run: `go build ./cmd/hop/... && CGO_ENABLED=0 GOOS=linux go build -o /dev/null ./cmd/hop-agent/...`
Expected: Both build cleanly.

**Step 3: Run linter**

Run: `golangci-lint run`
Expected: No issues.

**Step 4: Commit any remaining changes and update ROADMAP**

Mark the native service backend item as done in `ROADMAP.md`:

Change:
```
- [ ] Native service backend — run processes directly without Docker
```
To:
```
- [x] Native service backend — run processes directly without Docker
```

```
git add ROADMAP.md
git commit -m "docs: mark native service backend as complete"
```
