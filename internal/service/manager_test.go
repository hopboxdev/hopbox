package service_test

import (
	"context"
	"errors"
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

func TestManagerRegisterAndList(t *testing.T) {
	m := service.NewManager()
	m.Register(&service.ServiceDef{Name: "web", Type: "docker"}, &stubBackend{running: true})
	m.Register(&service.ServiceDef{Name: "db", Type: "native"}, &stubBackend{running: false})

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
	m.Register(&service.ServiceDef{Name: "app", Type: "native"}, b)

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
	m.Register(&service.ServiceDef{Name: "app", Type: "native"}, b)

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
	m.Register(&service.ServiceDef{Name: "app", Type: "native"}, b)

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
	m.Register(&service.ServiceDef{Name: "svc1", Type: "docker"}, b1)
	m.Register(&service.ServiceDef{Name: "svc2", Type: "docker"}, b2)

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
	m.Register(&service.ServiceDef{Name: "bad", Type: "native"}, b)

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
	m.Register(&service.ServiceDef{Name: "svc", Type: "docker"}, &errorBackend{err: boom})

	statuses := m.ListStatus()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Error == "" {
		t.Error("expected Error field to be set when IsRunning returns error")
	}
}

// errorBackend always returns an error from IsRunning.
type errorBackend struct{ err error }

func (e *errorBackend) Start(_ context.Context, _ string) error { return nil }
func (e *errorBackend) Stop(_ string) error                     { return nil }
func (e *errorBackend) IsRunning(_ string) (bool, error)        { return false, e.err }
