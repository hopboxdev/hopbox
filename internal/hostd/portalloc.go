package hostd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PortAllocator manages UDP port assignments for workspaces.
// Assignments are persisted to a JSON file so they survive daemon restarts.
type PortAllocator struct {
	mu      sync.Mutex
	minPort int
	maxPort int
	path    string         // persistence file path
	byName  map[string]int // workspace name -> port
	byPort  map[int]string // port -> workspace name
}

// NewPortAllocator creates a port allocator for the range [minPort, maxPort].
// If the persistence file exists, it loads previous assignments.
func NewPortAllocator(minPort, maxPort int, path string) (*PortAllocator, error) {
	pa := &PortAllocator{
		minPort: minPort,
		maxPort: maxPort,
		path:    path,
		byName:  make(map[string]int),
		byPort:  make(map[int]string),
	}
	if err := pa.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading port allocations: %w", err)
	}
	return pa, nil
}

// Allocate assigns the next available port to the named workspace.
func (pa *PortAllocator) Allocate(name string) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if _, exists := pa.byName[name]; exists {
		return 0, fmt.Errorf("workspace %q already has a port allocated", name)
	}

	for port := pa.minPort; port <= pa.maxPort; port++ {
		if _, used := pa.byPort[port]; !used {
			pa.byName[name] = port
			pa.byPort[port] = name
			if err := pa.save(); err != nil {
				delete(pa.byName, name)
				delete(pa.byPort, port)
				return 0, fmt.Errorf("persisting allocation: %w", err)
			}
			return port, nil
		}
	}

	return 0, fmt.Errorf("no ports available in range %d-%d", pa.minPort, pa.maxPort)
}

// Release frees the port assigned to the named workspace.
func (pa *PortAllocator) Release(name string) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, exists := pa.byName[name]
	if !exists {
		return nil // idempotent
	}

	delete(pa.byName, name)
	delete(pa.byPort, port)
	return pa.save()
}

// Get returns the port assigned to the named workspace.
func (pa *PortAllocator) Get(name string) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, exists := pa.byName[name]
	if !exists {
		return 0, fmt.Errorf("no port allocated for workspace %q", name)
	}
	return port, nil
}

func (pa *PortAllocator) load() error {
	data, err := os.ReadFile(pa.path)
	if err != nil {
		return err
	}
	var assignments map[string]int
	if err := json.Unmarshal(data, &assignments); err != nil {
		return err
	}
	for name, port := range assignments {
		pa.byName[name] = port
		pa.byPort[port] = name
	}
	return nil
}

func (pa *PortAllocator) save() error {
	data, err := json.Marshal(pa.byName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pa.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(pa.path, data, 0600)
}
