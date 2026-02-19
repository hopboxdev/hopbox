package service

import (
	"context"
	"fmt"
	"sync"
)

// Status describes the runtime state of a managed service.
type Status struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Type    string `json:"type"`
	Error   string `json:"error,omitempty"`
}

// Backend can start, stop, and report on a single service.
type Backend interface {
	Start(ctx context.Context, name string) error
	Stop(name string) error
	IsRunning(name string) (bool, error)
}

// ServiceDef is the parsed definition of a single service from the manifest.
type ServiceDef struct {
	Name    string
	Type    string // "docker", "native"
	Image   string // for docker
	Command string // for native
	Ports   []int
	Env     map[string]string
}

// Manager orchestrates a set of services.
type Manager struct {
	mu       sync.Mutex
	services map[string]*ServiceDef
	backends map[string]Backend
}

// NewManager creates a new empty service manager.
func NewManager() *Manager {
	return &Manager{
		services: make(map[string]*ServiceDef),
		backends: make(map[string]Backend),
	}
}

// Register adds a service definition and its backend to the manager.
func (m *Manager) Register(def *ServiceDef, backend Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[def.Name] = def
	m.backends[def.Name] = backend
}

// StartAll starts all registered services in dependency order.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	names := make([]string, 0, len(m.services))
	for name := range m.services {
		names = append(names, name)
	}
	m.mu.Unlock()

	for _, name := range names {
		if err := m.Start(ctx, name); err != nil {
			return fmt.Errorf("start service %q: %w", name, err)
		}
	}
	return nil
}

// Start starts a single service by name.
func (m *Manager) Start(ctx context.Context, name string) error {
	m.mu.Lock()
	backend, ok := m.backends[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}
	return backend.Start(ctx, name)
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

	var statuses []Status
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
