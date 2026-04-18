package containers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserImageTag_Deterministic(t *testing.T) {
	tag1, err := userImageTagFromFile("alice", "default", writeTemp(t, []byte(`{"name":"x"}`)))
	if err != nil {
		t.Fatalf("tag1: %v", err)
	}
	tag2, err := userImageTagFromFile("alice", "default", writeTemp(t, []byte(`{"name":"x"}`)))
	if err != nil {
		t.Fatalf("tag2: %v", err)
	}
	if tag1 != tag2 {
		t.Errorf("same content should produce same tag: %q vs %q", tag1, tag2)
	}
}

func TestUserImageTag_DiffersOnContent(t *testing.T) {
	tag1, _ := userImageTagFromFile("alice", "default", writeTemp(t, []byte(`{"name":"x"}`)))
	tag2, _ := userImageTagFromFile("alice", "default", writeTemp(t, []byte(`{"name":"y"}`)))
	if tag1 == tag2 {
		t.Errorf("different content should produce different tags, got identical %q", tag1)
	}
}

func TestUserImageTag_IncludesUsernameAndBox(t *testing.T) {
	tag, err := userImageTagFromFile("alice", "myproject", writeTemp(t, []byte(`{"name":"x"}`)))
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if !strings.HasPrefix(tag, "hopbox-alice-myproject:") {
		t.Errorf("expected prefix hopbox-alice-myproject:, got %q", tag)
	}
}

func writeTemp(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "devcontainer.json")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Compile-time assertion that EnsureUserImage has the expected shape.
var _ = func(ctx context.Context) {
	_, _ = EnsureUserImage(ctx, nil, "u", "b", "/path")
}
