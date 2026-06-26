package workspace

import (
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

func TestBuildFromSpecWiresLifetimeAndBackend(t *testing.T) {
	spec, err := box.ParseSpec("proj~docker:python+10m")
	if err != nil {
		t.Fatal(err)
	}
	w, err := BuildFromSpec(spec, "default", "alice", "alpine", []string{"docker", "k8s"}, "")
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

func TestBuildFromSpecDefaultsImageAndDiesOnDisconnect(t *testing.T) {
	spec, _ := box.ParseSpec("proj") // no image, no duration
	w, err := BuildFromSpec(spec, "default", "alice", "alpine", []string{"docker"}, "")
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

func TestBuildFromSpecRejectsSpecial(t *testing.T) {
	spec, _ := box.ParseSpec("cli")
	if _, err := BuildFromSpec(spec, "default", "alice", "alpine", []string{"docker"}, ""); err == nil {
		t.Error("BuildFromSpec on a special username (cli) must error")
	}
}
