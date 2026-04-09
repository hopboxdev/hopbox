# Hopbox Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add idle timeout (auto-stop containers with no SSH sessions after N hours) and per-container resource limits (CPU, memory, PIDs) configured server-wide.

**Architecture:** Config gets new fields for timeout and resources. Manager tracks active session counts per container with idle timers. Resource limits are applied via Docker's HostConfig.Resources on container creation. Session handler notifies manager on connect/disconnect.

**Tech Stack:** Go, Docker SDK (container.Resources), time.AfterFunc

---

## File Structure

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `IdleTimeoutHours` and `ResourcesConfig` fields |
| `internal/config/config_test.go` | Test new config fields |
| `internal/containers/manager.go` | Add session tracking, idle timers, resource limits in HostConfig |
| `internal/containers/manager_test.go` | Test session tracking logic |
| `internal/gateway/server.go` | Call SessionConnect/SessionDisconnect |
| `cmd/hopboxd/main.go` | Pass config to Manager |

---

### Task 1: Config Fields

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test for new config fields**

Add to `internal/config/config_test.go`:

```go
func TestLoadResourceDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IdleTimeoutHours != 24 {
		t.Errorf("idle timeout: got %d, want 24", cfg.IdleTimeoutHours)
	}
	if cfg.Resources.CPUCores != 2 {
		t.Errorf("cpu cores: got %d, want 2", cfg.Resources.CPUCores)
	}
	if cfg.Resources.MemoryGB != 4 {
		t.Errorf("memory gb: got %d, want 4", cfg.Resources.MemoryGB)
	}
	if cfg.Resources.PidsLimit != 512 {
		t.Errorf("pids limit: got %d, want 512", cfg.Resources.PidsLimit)
	}
}

func TestLoadResourcesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`
port = 2222
idle_timeout_hours = 12

[resources]
cpu_cores = 4
memory_gb = 8
pids_limit = 1024
`), 0644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IdleTimeoutHours != 12 {
		t.Errorf("idle timeout: got %d, want 12", cfg.IdleTimeoutHours)
	}
	if cfg.Resources.CPUCores != 4 {
		t.Errorf("cpu cores: got %d, want 4", cfg.Resources.CPUCores)
	}
	if cfg.Resources.MemoryGB != 8 {
		t.Errorf("memory gb: got %d, want 8", cfg.Resources.MemoryGB)
	}
	if cfg.Resources.PidsLimit != 1024 {
		t.Errorf("pids limit: got %d, want 1024", cfg.Resources.PidsLimit)
	}
}

func TestLoadIdleTimeoutZeroDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`
idle_timeout_hours = 0
`), 0644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IdleTimeoutHours != 0 {
		t.Errorf("idle timeout: got %d, want 0 (disabled)", cfg.IdleTimeoutHours)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -v -run "TestLoadResource|TestLoadIdleTimeout"
```

Expected: FAIL — `IdleTimeoutHours` and `Resources` fields don't exist.

- [ ] **Step 3: Implement config changes**

Update `internal/config/config.go`:

```go
type ResourcesConfig struct {
	CPUCores  int   `toml:"cpu_cores"`
	MemoryGB  int   `toml:"memory_gb"`
	PidsLimit int64 `toml:"pids_limit"`
}

type Config struct {
	Port             int             `toml:"port"`
	DataDir          string          `toml:"data_dir"`
	HostKeyPath      string          `toml:"host_key_path"`
	OpenRegistration bool            `toml:"open_registration"`
	IdleTimeoutHours int             `toml:"idle_timeout_hours"`
	Resources        ResourcesConfig `toml:"resources"`
}

func defaults() Config {
	return Config{
		Port:             2222,
		DataDir:          "./data",
		HostKeyPath:      "",
		OpenRegistration: true,
		IdleTimeoutHours: 24,
		Resources: ResourcesConfig{
			CPUCores:  2,
			MemoryGB:  4,
			PidsLimit: 512,
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add idle timeout and resource limits to config"
```

---

### Task 2: Session Tracking & Idle Timeout + Resource Limits in Manager

**Files:**
- Modify: `internal/containers/manager.go`
- Modify: `internal/containers/manager_test.go`

- [ ] **Step 1: Write the failing test for session tracking**

Add to `internal/containers/manager_test.go`:

```go
func TestSessionTracking(t *testing.T) {
	m := &Manager{
		states: make(map[string]*containerState),
	}

	// Connect increments
	m.SessionConnect("container-1")
	m.mu.Lock()
	s := m.states["container-1"]
	m.mu.Unlock()
	if s == nil || s.sessions != 1 {
		t.Fatalf("expected 1 session, got %v", s)
	}

	// Second connect
	m.SessionConnect("container-1")
	m.mu.Lock()
	s = m.states["container-1"]
	m.mu.Unlock()
	if s.sessions != 2 {
		t.Errorf("expected 2 sessions, got %d", s.sessions)
	}

	// Disconnect
	m.SessionDisconnect("container-1")
	m.mu.Lock()
	s = m.states["container-1"]
	m.mu.Unlock()
	if s.sessions != 1 {
		t.Errorf("expected 1 session, got %d", s.sessions)
	}

	// Last disconnect (no idle timeout configured)
	m.SessionDisconnect("container-1")
	m.mu.Lock()
	s = m.states["container-1"]
	m.mu.Unlock()
	if s.sessions != 0 {
		t.Errorf("expected 0 sessions, got %d", s.sessions)
	}
}

func TestSessionConnectCancelsIdleTimer(t *testing.T) {
	m := &Manager{
		states:      make(map[string]*containerState),
		idleTimeout: 1 * time.Hour,
	}

	// Connect then disconnect to start timer
	m.SessionConnect("container-1")
	m.SessionDisconnect("container-1")

	m.mu.Lock()
	s := m.states["container-1"]
	hasTimer := s.idleTimer != nil
	m.mu.Unlock()
	if !hasTimer {
		t.Error("expected idle timer to be set")
	}

	// Reconnect should cancel the timer
	m.SessionConnect("container-1")
	m.mu.Lock()
	s = m.states["container-1"]
	hasTimer = s.idleTimer != nil
	m.mu.Unlock()
	if hasTimer {
		t.Error("expected idle timer to be cancelled on reconnect")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/containers/ -v -run "TestSession"
```

Expected: FAIL — `containerState`, `SessionConnect`, `SessionDisconnect`, `states`, `idleTimeout` not defined.

- [ ] **Step 3: Implement session tracking and resource limits**

Update `internal/containers/manager.go`:

Add the `containerState` type and update `Manager`:

```go
type containerState struct {
	sessions  int
	idleTimer *time.Timer
}

type Manager struct {
	cli         *client.Client
	sockets     map[string]*control.SocketServer
	states      map[string]*containerState
	mu          sync.Mutex
	idleTimeout time.Duration
	resources   config.ResourcesConfig
}

func NewManager(cli *client.Client, cfg config.Config) *Manager {
	var timeout time.Duration
	if cfg.IdleTimeoutHours > 0 {
		timeout = time.Duration(cfg.IdleTimeoutHours) * time.Hour
	}
	return &Manager{
		cli:         cli,
		sockets:     make(map[string]*control.SocketServer),
		states:      make(map[string]*containerState),
		idleTimeout: timeout,
		resources:   cfg.Resources,
	}
}
```

Add import for `"github.com/hopboxdev/hopbox/internal/config"`.

Add session tracking methods:

```go
// SessionConnect increments the session count for a container and cancels any idle timer.
func (m *Manager) SessionConnect(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.states[containerID]
	if !ok {
		s = &containerState{}
		m.states[containerID] = s
	}
	s.sessions++

	// Cancel idle timer if running
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
		log.Printf("[idle] cancelled idle timer for container %s (session reconnected)", containerID[:12])
	}
}

// SessionDisconnect decrements the session count and starts an idle timer if no sessions remain.
func (m *Manager) SessionDisconnect(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.states[containerID]
	if !ok {
		return
	}
	s.sessions--
	if s.sessions < 0 {
		s.sessions = 0
	}

	if s.sessions == 0 && m.idleTimeout > 0 {
		log.Printf("[idle] starting %v idle timer for container %s", m.idleTimeout, containerID[:12])
		s.idleTimer = time.AfterFunc(m.idleTimeout, func() {
			m.stopIdleContainer(containerID)
		})
	}
}

func (m *Manager) stopIdleContainer(containerID string) {
	log.Printf("[idle] stopping idle container %s", containerID[:12])
	ctx := context.Background()
	if err := m.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		log.Printf("[idle] failed to stop container %s: %v", containerID[:12], err)
	}

	// Clean up state
	m.mu.Lock()
	delete(m.states, containerID)
	m.mu.Unlock()
}
```

Update the `HostConfig` in `EnsureRunning` to include resource limits. In the container creation section, change:

```go
	hostCfg := &container.HostConfig{
		Binds: []string{
			fmt.Sprintf("%s:/home/dev", homePath),
			fmt.Sprintf("%s:/var/run/hopbox", socketDir),
		},
		Resources: m.resourceLimits(),
	}
```

Add the helper:

```go
func (m *Manager) resourceLimits() container.Resources {
	var pidsLimit *int64
	if m.resources.PidsLimit > 0 {
		pl := m.resources.PidsLimit
		pidsLimit = &pl
	}

	r := container.Resources{
		PidsLimit: pidsLimit,
	}
	if m.resources.CPUCores > 0 {
		r.NanoCPUs = int64(m.resources.CPUCores) * 1_000_000_000
	}
	if m.resources.MemoryGB > 0 {
		r.Memory = int64(m.resources.MemoryGB) * 1024 * 1024 * 1024
	}
	return r
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/containers/ -v -run "TestSession"
```

Expected: all session tracking tests PASS.

- [ ] **Step 5: Verify the package compiles**

```bash
go build ./internal/containers/
```

Expected: compiles. The gateway package will break (NewManager signature changed) — Task 3 fixes that.

- [ ] **Step 6: Commit**

```bash
git add internal/containers/manager.go internal/containers/manager_test.go
git commit -m "feat: add session tracking, idle timeout, and resource limits to manager"
```

---

### Task 3: Wire Session Tracking into Session Handler

**Files:**
- Modify: `internal/gateway/server.go`
- Modify: `cmd/hopboxd/main.go`

- [ ] **Step 1: Update main.go to pass config to NewManager**

In `cmd/hopboxd/main.go`, change:

```go
mgr := containers.NewManager(cli)
```

to:

```go
mgr := containers.NewManager(cli, cfg)
```

- [ ] **Step 2: Add SessionConnect/SessionDisconnect calls to server.go**

In `internal/gateway/server.go`, in the `sessionHandler` function:

After the line `ctx.SetValue("container_id", containerID)` (around line 271), add:

```go
	s.manager.SessionConnect(containerID)
```

Before the line `log.Printf("[session] disconnect user=%s box=%s", ...)` (around line 329), add:

```go
	s.manager.SessionDisconnect(containerID)
```

The disconnect must happen in all exit paths. Wrap the exec section with a defer:

After `s.manager.SessionConnect(containerID)`, add:

```go
	defer s.manager.SessionDisconnect(containerID)
```

Then remove the explicit `SessionDisconnect` before the disconnect log (the defer handles it).

- [ ] **Step 3: Verify the whole project compiles**

```bash
go build ./...
```

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/server.go cmd/hopboxd/main.go
git commit -m "feat: wire session tracking into session handler for idle timeout"
```

---

### Task 4: Integration Smoke Test

**Files:** None (manual testing)

- [ ] **Step 1: Create a test config with short idle timeout**

Create `config.toml`:

```toml
port = 2222
idle_timeout_hours = 0

[resources]
cpu_cores = 1
memory_gb = 2
pids_limit = 256
```

Note: `idle_timeout_hours = 0` disables the timeout for now. To test the actual timeout, temporarily set it to a very small value — but since it's in hours, testing requires modifying the code to use minutes or seconds. For the smoke test, verify that:

1. Resource limits are applied to new containers
2. Session connect/disconnect is logged
3. The timeout timer starts on disconnect (check server logs)

- [ ] **Step 2: Build and run with the config**

```bash
/usr/local/bin/docker rm -f $(/usr/local/bin/docker ps -aq --filter "name=hopbox-") 2>/dev/null
rm -rf data/users/
go build -o hopboxd ./cmd/hopboxd/ && ./hopboxd
```

- [ ] **Step 3: Connect and verify resource limits**

```bash
ssh -p 2222 hop@localhost
```

Inside the container, check resource limits:

```bash
cat /sys/fs/cgroup/cpu.max      # should show CPU limit
cat /sys/fs/cgroup/memory.max   # should show memory limit
cat /sys/fs/cgroup/pids.max     # should show 256
```

- [ ] **Step 4: Verify session tracking in server logs**

Connect and disconnect. Server logs should show session connect/disconnect events. If idle timeout is configured (non-zero), logs should show timer start on disconnect.

- [ ] **Step 5: Clean up test config and commit fixes**

```bash
rm config.toml
git add -A
git commit -m "fix: address issues found during Phase 4 integration testing"
```

(Only if fixes were needed.)

---

## Task Dependency Graph

```
Task 1 (Config) ──► Task 2 (Manager: sessions + resources) ──► Task 3 (Wire into server) ──► Task 4 (Smoke Test)
```

Linear — each task depends on the previous. Task 2 uses the new config types, Task 3 uses the new Manager methods.
