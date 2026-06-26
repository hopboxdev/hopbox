package workspace

import "testing"

// New wraps a box and the dev-env fields default empty; box fields promote.
func TestNewWrapsBox(t *testing.T) {
	w := New("default", "alice", "proj", "ubuntu:24.04")
	if w.ID == "" || w.Name != "proj" || w.ImageRef != "ubuntu:24.04" {
		t.Fatalf("box fields not promoted: %+v", w)
	}
	if w.HomeMount != "" || w.Ingress != nil || w.Endpoints != nil {
		t.Fatalf("dev-env fields should default empty: %+v", w)
	}
}
