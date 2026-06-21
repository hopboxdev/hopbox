package subdomain

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
)

func TestExposeBuildsHostAndLookupResolves(t *testing.T) {
	p := New("gw.example.com")
	ep, err := p.Expose(context.Background(), ports.ExposeRequest{WorkspaceID: "w1", Name: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "https://app-w1.gw.example.com" || ep.Ref != "app-w1.gw.example.com" {
		t.Fatalf("bad endpoint: %+v", ep)
	}
	rt, ok := p.Lookup("app-w1.gw.example.com")
	if !ok || rt.WorkspaceID != "w1" || rt.Port != 3000 {
		t.Fatalf("lookup miss/wrong: %+v ok=%v", rt, ok)
	}
}

func TestUnexposeRemovesFromTable(t *testing.T) {
	p := New("gw.example.com")
	ep, _ := p.Expose(context.Background(), ports.ExposeRequest{WorkspaceID: "w1", Name: "app", Port: 3000})
	if err := p.Unexpose(context.Background(), ep.Ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Lookup(ep.Ref); ok {
		t.Fatalf("route still present after unexpose")
	}
}

func TestExposeRejectsBadInput(t *testing.T) {
	p := New("gw.example.com")
	if _, err := p.Expose(context.Background(), ports.ExposeRequest{WorkspaceID: "w1", Name: "app", Port: 0}); err == nil {
		t.Fatal("expected error for port 0")
	}
	if _, err := p.Expose(context.Background(), ports.ExposeRequest{Name: "app", Port: 80}); err == nil {
		t.Fatal("expected error for empty workspace id")
	}
}
