package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Status describes the runtime state of a managed service.
type Status struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Type    string `json:"type"`
	Error   string `json:"error,omitempty"`
}

// HealthCheck configures readiness polling after a service starts.
type HealthCheck struct {
	HTTP     string        // URL to poll
	Interval time.Duration // polling interval (default 2s)
	Timeout  time.Duration // overall timeout (default 60s)
}

// Backend can start, stop, and report on a single service.
type Backend interface {
	Start(ctx context.Context, name string) error
	Stop(name string) error
	IsRunning(name string) (bool, error)
}

// Def is the parsed definition of a single service from the manifest.
type Def struct {
	Name      string
	Type      string // "docker", "native"
	Image     string // for docker
	Command   string // for native
	Ports     []string
	Env       map[string]string
	DependsOn []string
	Health    *HealthCheck
	DataPaths []string // host paths for backup
}

// Manager orchestrates a set of services.
type Manager struct {
	mu       sync.Mutex
	services map[string]*Def
	backends map[string]Backend
}

// NewManager creates a new empty service manager.
func NewManager() *Manager {
	return &Manager{
		services: make(map[string]*Def),
		backends: make(map[string]Backend),
	}
}

// Register adds a service definition and its backend to the manager.
func (m *Manager) Register(def *Def, backend Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[def.Name] = def
	m.backends[def.Name] = backend
}

// StartAll starts all registered services in dependency order.
func (m *Manager) StartAll(ctx context.Context) error {
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

	for _, name := range order {
		if err := m.Start(ctx, name); err != nil {
			return fmt.Errorf("start service %q: %w", name, err)
		}
	}
	return nil
}

// Start starts a single service by name, then waits for it to be healthy.
// If the service is already running it is a no-op.
func (m *Manager) Start(ctx context.Context, name string) error {
	m.mu.Lock()
	backend, ok := m.backends[name]
	def := m.services[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}
	if running, err := backend.IsRunning(name); err == nil && running {
		return nil
	}
	if err := backend.Start(ctx, name); err != nil {
		return err
	}
	if def.Health != nil && def.Health.HTTP != "" {
		if err := waitHealthy(ctx, def.Health); err != nil {
			return fmt.Errorf("service %q health check: %w", name, err)
		}
	}
	return nil
}

// Stop stops a single service by name.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	backend, ok := m.backends[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}
	return backend.Stop(name)
}

// Restart stops then starts a service.
func (m *Manager) Restart(name string) error {
	_ = m.Stop(name) // ignore stop errors
	return m.Start(context.Background(), name)
}

// ListStatus returns the runtime status of all services.
func (m *Manager) ListStatus() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	statuses := make([]Status, 0, len(m.services))
	for name, def := range m.services {
		s := Status{Name: name, Type: def.Type}
		if backend, ok := m.backends[name]; ok {
			running, err := backend.IsRunning(name)
			s.Running = running
			if err != nil {
				s.Error = err.Error()
			}
		}
		statuses = append(statuses, s)
	}
	return statuses
}

// DataPaths returns the union of all DataPaths across registered services.
// Used by the snapshot subsystem to determine what to back up.
func (m *Manager) DataPaths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[string]struct{})
	var paths []string
	for _, def := range m.services {
		for _, p := range def.DataPaths {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				paths = append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

// topoSort returns service names in dependency-safe start order using
// Kahn's algorithm. Returns an error on unknown dependencies or cycles.
func topoSort(defs map[string]*Def) ([]string, error) {
	inDegree := make(map[string]int, len(defs))
	for name := range defs {
		inDegree[name] = 0
	}
	for _, def := range defs {
		for _, dep := range def.DependsOn {
			if _, ok := defs[dep]; !ok {
				return nil, fmt.Errorf("service %q depends on unknown service %q", def.Name, dep)
			}
			inDegree[def.Name]++
		}
	}

	// Seed queue with zero-in-degree nodes, sorted for determinism.
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)

		// Decrement in-degree for dependents.
		var next []string
		for _, def := range defs {
			for _, dep := range def.DependsOn {
				if dep == name {
					inDegree[def.Name]--
					if inDegree[def.Name] == 0 {
						next = append(next, def.Name)
					}
				}
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}

	if len(order) != len(defs) {
		return nil, fmt.Errorf("dependency cycle detected")
	}
	return order, nil
}

// waitHealthy polls hc.HTTP until it returns HTTP 200 or the timeout expires.
func waitHealthy(ctx context.Context, hc *HealthCheck) error {
	interval := hc.Interval
	if interval == 0 {
		interval = 2 * time.Second
	}
	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for {
		resp, err := client.Get(hc.HTTP)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("not healthy within %s: %w", timeout, lastErr)
		case <-time.After(interval):
		}
	}
}
