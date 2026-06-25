package workspace

import (
	"testing"
	"time"
)

func TestParseSpecGrammar(t *testing.T) {
	cases := []struct {
		in   string
		want UserSpec
	}{
		{"myproject", UserSpec{Workspace: "myproject"}},
		{"myproject:python", UserSpec{Workspace: "myproject", Image: "python"}},
		{"myproject~ovh:python:l4", UserSpec{Workspace: "myproject", Backend: "ovh", Image: "python", Flavor: "l4"}},
		{"myproject:go:cpu+5m", UserSpec{Workspace: "myproject", Image: "go", Flavor: "cpu", Grace: 5 * time.Minute}},
		{"myproject~runtime:go:cpu+1h", UserSpec{Workspace: "myproject", Backend: "runtime", Image: "go", Flavor: "cpu", Grace: time.Hour}},
		{"myproject+", UserSpec{Workspace: "myproject", ForceNew: true}},
		{"myproject~ovh+", UserSpec{Workspace: "myproject", Backend: "ovh", ForceNew: true}},
	}
	for _, c := range cases {
		got, err := ParseSpec(c.in)
		if err != nil {
			t.Errorf("ParseSpec(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseSpecSpecials(t *testing.T) {
	cases := []struct {
		in        string
		special   string
		sessionID string
	}{
		{"cli", "cli", ""},
		{"sudo", "sudo", ""},
		{"_", "_", ""},
		{"session-abc123", "session", "abc123"},
	}
	for _, c := range cases {
		got, err := ParseSpec(c.in)
		if err != nil {
			t.Errorf("ParseSpec(%q) error: %v", c.in, err)
			continue
		}
		if got.Special != c.special || got.SessionID != c.sessionID {
			t.Errorf("ParseSpec(%q) special=%q session=%q, want %q/%q", c.in, got.Special, got.SessionID, c.special, c.sessionID)
		}
	}
}

func TestParseSpecRejectsEmptyAndBadDuration(t *testing.T) {
	if _, err := ParseSpec(""); err == nil {
		t.Error("empty username must error")
	}
	if _, err := ParseSpec("proj:go:cpu+nonsense"); err == nil {
		t.Error("bad duration must error")
	}
}

func TestBuildWorkspaceWiresLifetimeAndBackend(t *testing.T) {
	spec, err := ParseSpec("proj~docker:python+10m")
	if err != nil {
		t.Fatal(err)
	}
	w, err := spec.BuildWorkspace("default", "alice", "alpine", []string{"docker", "k8s"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "proj" || w.ImageRef != "python" {
		t.Fatalf("name=%q image=%q", w.Name, w.ImageRef)
	}
	if w.Backend != "docker" {
		t.Fatalf("backend=%q want docker", w.Backend)
	}
	// SSH-spawned boxes are temporary: ephemeral with the parsed grace.
	if !w.Ephemeral || w.Grace != 10*time.Minute {
		t.Fatalf("ephemeral=%v grace=%v", w.Ephemeral, w.Grace)
	}
}

func TestBuildWorkspaceDefaultsImageAndDiesOnDisconnect(t *testing.T) {
	spec, _ := ParseSpec("proj") // no image, no duration
	w, err := spec.BuildWorkspace("default", "alice", "alpine", []string{"docker"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if w.ImageRef != "alpine" {
		t.Fatalf("image=%q want alpine default", w.ImageRef)
	}
	if !w.Ephemeral || w.Grace != 0 {
		t.Fatalf("expected ephemeral grace=0 (die on disconnect), got ephemeral=%v grace=%v", w.Ephemeral, w.Grace)
	}
	if w.Backend != "docker" {
		t.Fatalf("backend=%q want docker (sole backend, auto)", w.Backend)
	}
}

func TestBuildWorkspaceRejectsSpecial(t *testing.T) {
	spec, _ := ParseSpec("cli")
	if _, err := spec.BuildWorkspace("default", "alice", "alpine", []string{"docker"}, ""); err == nil {
		t.Error("BuildWorkspace on a special username (cli) must error")
	}
}
