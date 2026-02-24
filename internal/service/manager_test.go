package service_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/hopboxdev/hopbox/internal/service"
)

// stubBackend records calls for assertions.
type stubBackend struct {
	running bool
	startFn func() error
	stopFn  func() error
}

func (s *stubBackend) Start(_ context.Context, _ string) error {
	if s.startFn != nil {
		return s.startFn()
	}
	s.running = true
	return nil
}

func (s *stubBackend) Stop(_ string) error {
	if s.stopFn != nil {
		return s.stopFn()
	}
	s.running = false
	return nil
}

func (s *stubBackend) IsRunning(_ string) (bool, error) {
	return s.running, nil
}

func (s *stubBackend) LogCmd(_ string, _ int) *exec.Cmd { return nil }

func TestManagerRegisterAndList(t *testing.T) {
	m := service.NewManager()
	m.Register(&service.Def{Name: "web", Type: "docker"}, &stubBackend{running: true})
	m.Register(&service.Def{Name: "db", Type: "native"}, &stubBackend{running: false})

	statuses := m.ListStatus()
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	byName := map[string]service.Status{}
	for _, s := range statuses {
		byName[s.Name] = s
	}
	if !byName["web"].Running {
		t.Error("web should be running")
	}
	if byName["db"].Running {
		t.Error("db should not be running")
	}
}

func TestManagerStart(t *testing.T) {
	m := service.NewManager()
	b := &stubBackend{}
	m.Register(&service.Def{Name: "app", Type: "native"}, b)

	if err := m.Start(context.Background(), "app"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !b.running {
		t.Error("backend should be running after Start")
	}
}

func TestManagerStop(t *testing.T) {
	m := service.NewManager()
	b := &stubBackend{running: true}
	m.Register(&service.Def{Name: "app", Type: "native"}, b)

	if err := m.Stop("app"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if b.running {
		t.Error("backend should not be running after Stop")
	}
}

func TestManagerRestart(t *testing.T) {
	m := service.NewManager()
	calls := 0
	b := &stubBackend{
		running: true,
		startFn: func() error { calls++; return nil },
	}
	m.Register(&service.Def{Name: "app", Type: "native"}, b)

	if err := m.Restart("app"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if calls != 1 {
		t.Errorf("startFn called %d times, want 1", calls)
	}
}

func TestManagerStartUnknownService(t *testing.T) {
	m := service.NewManager()
	err := m.Start(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestManagerStopUnknownService(t *testing.T) {
	m := service.NewManager()
	err := m.Stop("nonexistent")
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestManagerStartAll(t *testing.T) {
	m := service.NewManager()
	b1 := &stubBackend{}
	b2 := &stubBackend{}
	m.Register(&service.Def{Name: "svc1", Type: "docker"}, b1)
	m.Register(&service.Def{Name: "svc2", Type: "docker"}, b2)

	if err := m.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if !b1.running {
		t.Error("svc1 should be running")
	}
	if !b2.running {
		t.Error("svc2 should be running")
	}
}

func TestManagerStartAllPropagatesError(t *testing.T) {
	m := service.NewManager()
	boom := errors.New("start failed")
	b := &stubBackend{startFn: func() error { return boom }}
	m.Register(&service.Def{Name: "bad", Type: "native"}, b)

	err := m.StartAll(context.Background())
	if err == nil {
		t.Error("expected error from StartAll")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want wrapping %v", err, boom)
	}
}

func TestManagerListStatusWithError(t *testing.T) {
	m := service.NewManager()
	boom := errors.New("docker unavailable")
	m.Register(&service.Def{Name: "svc", Type: "docker"}, &errorBackend{err: boom})

	statuses := m.ListStatus()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Error == "" {
		t.Error("expected Error field to be set when IsRunning returns error")
	}
}

func TestManagerStartAllDependencyOrder(t *testing.T) {
	m := service.NewManager()
	var startOrder []string

	for _, name := range []string{"db", "cache", "api"} {
		n := name // capture
		b := &stubBackend{
			startFn: func() error {
				startOrder = append(startOrder, n)
				return nil
			},
		}
		var deps []string
		if n == "api" {
			deps = []string{"db", "cache"}
		}
		m.Register(&service.Def{Name: n, Type: "docker", DependsOn: deps}, b)
	}

	if err := m.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// api must start after both db and cache
	apiIdx := -1
	for i, name := range startOrder {
		if name == "api" {
			apiIdx = i
		}
	}
	for _, dep := range []string{"db", "cache"} {
		depIdx := -1
		for i, name := range startOrder {
			if name == dep {
				depIdx = i
			}
		}
		if depIdx >= apiIdx {
			t.Errorf("expected %s to start before api (start order: %v)", dep, startOrder)
		}
	}
}

func TestManagerStartAllCycleError(t *testing.T) {
	m := service.NewManager()
	m.Register(&service.Def{Name: "a", Type: "native", DependsOn: []string{"b"}}, &stubBackend{})
	m.Register(&service.Def{Name: "b", Type: "native", DependsOn: []string{"a"}}, &stubBackend{})

	err := m.StartAll(context.Background())
	if err == nil {
		t.Error("expected error for dependency cycle")
	}
}

func TestManagerStartAllUnknownDep(t *testing.T) {
	m := service.NewManager()
	m.Register(&service.Def{Name: "api", Type: "native", DependsOn: []string{"db"}}, &stubBackend{})

	err := m.StartAll(context.Background())
	if err == nil {
		t.Error("expected error for unknown dependency")
	}
}

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

// errorBackend always returns an error from IsRunning.
type errorBackend struct{ err error }

func (e *errorBackend) Start(_ context.Context, _ string) error { return nil }
func (e *errorBackend) Stop(_ string) error                     { return nil }
func (e *errorBackend) IsRunning(_ string) (bool, error)        { return false, e.err }
func (e *errorBackend) LogCmd(_ string, _ int) *exec.Cmd        { return nil }
