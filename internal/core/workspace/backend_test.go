package workspace

import "testing"

func TestResolveBackendSingleIsDeducible(t *testing.T) {
	// "auto" (empty request) with exactly one backend resolves to it: the user
	// never has to name a backend when there is only one.
	got, err := ResolveBackend("", []string{"docker"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "docker" {
		t.Fatalf("got %q want docker", got)
	}
}

func TestResolveBackendExplicitMustExist(t *testing.T) {
	if _, err := ResolveBackend("k8s", []string{"docker"}, ""); err == nil {
		t.Fatal("expected error: requested backend not available")
	}
	got, err := ResolveBackend("docker", []string{"docker", "k8s"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "docker" {
		t.Fatalf("got %q want docker", got)
	}
}

func TestResolveBackendAutoMultipleUsesDefault(t *testing.T) {
	got, err := ResolveBackend("", []string{"docker", "k8s"}, "k8s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "k8s" {
		t.Fatalf("got %q want k8s (configured default)", got)
	}
}

func TestResolveBackendAutoMultipleNoDefaultIsAmbiguous(t *testing.T) {
	if _, err := ResolveBackend("", []string{"docker", "k8s"}, ""); err == nil {
		t.Fatal("expected ambiguity error: multiple backends, no default, none requested")
	}
}

func TestResolveBackendNoneAvailable(t *testing.T) {
	if _, err := ResolveBackend("", nil, ""); err == nil {
		t.Fatal("expected error: no compute backends configured")
	}
}
