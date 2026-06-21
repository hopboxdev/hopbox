package prebuilt

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
)

func TestResolvePassesImageThrough(t *testing.T) {
	p := New()
	img, err := p.Resolve(context.Background(), ports.BuildRequest{SourceRef: "ghcr.io/acme/dev:1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if img.Ref != "ghcr.io/acme/dev:1.2.3" || img.BuildRef != "" {
		t.Fatalf("expected pass-through with empty build ref: %+v", img)
	}
	st, err := p.Status(context.Background(), img.Ref)
	if err != nil || st.Phase != "ready" || st.ImageRef != img.Ref {
		t.Fatalf("prebuilt must be ready: %+v err=%v", st, err)
	}
}

func TestResolveRequiresSource(t *testing.T) {
	if _, err := New().Resolve(context.Background(), ports.BuildRequest{}); err == nil {
		t.Fatal("expected error when source_ref is empty")
	}
}
