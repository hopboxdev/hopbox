package containers

import (
	"testing"
	"time"
)

func TestShouldRecreate(t *testing.T) {
	tests := []struct {
		name           string
		containerLabel string
		wantHash       string
		want           bool
	}{
		{"matching", "abc123", "abc123", false},
		{"different hash", "abc123", "def456", true},
		{"no label", "", "def456", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRecreate(tt.containerLabel, tt.wantHash)
			if got != tt.want {
				t.Errorf("ShouldRecreate(%q, %q) = %v, want %v", tt.containerLabel, tt.wantHash, got, tt.want)
			}
		})
	}
}

func TestContainerName(t *testing.T) {
	tests := []struct {
		username string
		boxname  string
		want     string
	}{
		{"gandalf", "default", "hopbox-gandalf-default"},
		{"user", "myproject", "hopbox-user-myproject"},
	}
	for _, tt := range tests {
		got := ContainerName(tt.username, tt.boxname)
		if got != tt.want {
			t.Errorf("ContainerName(%q, %q) = %q, want %q", tt.username, tt.boxname, got, tt.want)
		}
	}
}

func TestSessionTracking(t *testing.T) {
	m := &Manager{
		states: make(map[string]*containerState),
	}

	m.SessionConnect("container-1234567890ab", "gandalf", "default")
	m.mu.Lock()
	s := m.states["container-1234567890ab"]
	m.mu.Unlock()
	if s == nil || s.sessions != 1 {
		t.Fatalf("expected 1 session, got %v", s)
	}

	m.SessionConnect("container-1234567890ab", "gandalf", "default")
	m.mu.Lock()
	s = m.states["container-1234567890ab"]
	m.mu.Unlock()
	if s.sessions != 2 {
		t.Errorf("expected 2 sessions, got %d", s.sessions)
	}

	m.SessionDisconnect("container-1234567890ab", "gandalf", "default")
	m.mu.Lock()
	s = m.states["container-1234567890ab"]
	m.mu.Unlock()
	if s.sessions != 1 {
		t.Errorf("expected 1 session, got %d", s.sessions)
	}

	m.SessionDisconnect("container-1234567890ab", "gandalf", "default")
	m.mu.Lock()
	s = m.states["container-1234567890ab"]
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

	m.SessionConnect("container-1234567890ab", "gandalf", "default")
	m.SessionDisconnect("container-1234567890ab", "gandalf", "default")

	m.mu.Lock()
	s := m.states["container-1234567890ab"]
	hasTimer := s.idleTimer != nil
	m.mu.Unlock()
	if !hasTimer {
		t.Error("expected idle timer to be set")
	}

	m.SessionConnect("container-1234567890ab", "gandalf", "default")
	m.mu.Lock()
	s = m.states["container-1234567890ab"]
	hasTimer = s.idleTimer != nil
	m.mu.Unlock()
	if hasTimer {
		t.Error("expected idle timer to be cancelled on reconnect")
	}
}
