package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hopboxdev/hopbox/internal/manifest"
)

func TestInstallBridgeScripts(t *testing.T) {
	dir := t.TempDir()
	ws := &manifest.Workspace{
		Bridges: []manifest.Bridge{
			{Type: "clipboard"},     // should NOT produce a script
			{Type: "xdg-open"},      // should produce xdg-open
			{Type: "notifications"}, // should produce notify-send
		},
	}

	if err := installBridgeScriptsTo(ws, dir); err != nil {
		t.Fatalf("installBridgeScriptsTo: %v", err)
	}

	// Expect exactly 2 scripts.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 scripts, got %d", len(entries))
	}

	// Verify xdg-open script.
	xdg, err := os.ReadFile(filepath.Join(dir, "xdg-open"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(xdg), "10.10.0.1") {
		t.Error("xdg-open script missing client IP")
	}
	if !strings.Contains(string(xdg), "2225") {
		t.Error("xdg-open script missing port 2225")
	}
	info, _ := os.Stat(filepath.Join(dir, "xdg-open"))
	if info.Mode().Perm()&0111 == 0 {
		t.Error("xdg-open script is not executable")
	}

	// Verify notify-send script.
	ns, err := os.ReadFile(filepath.Join(dir, "notify-send"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ns), "10.10.0.1") {
		t.Error("notify-send script missing client IP")
	}
	if !strings.Contains(string(ns), "2226") {
		t.Error("notify-send script missing port 2226")
	}
	info, _ = os.Stat(filepath.Join(dir, "notify-send"))
	if info.Mode().Perm()&0111 == 0 {
		t.Error("notify-send script is not executable")
	}
}

func TestInstallBridgeScripts_NilWorkspace(t *testing.T) {
	if err := installBridgeScriptsTo(nil, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallBridgeScripts_Idempotent(t *testing.T) {
	dir := t.TempDir()
	ws := &manifest.Workspace{
		Bridges: []manifest.Bridge{
			{Type: "xdg-open"},
		},
	}

	if err := installBridgeScriptsTo(ws, dir); err != nil {
		t.Fatal(err)
	}
	content1, _ := os.ReadFile(filepath.Join(dir, "xdg-open"))

	// Install again — should succeed and produce the same content.
	if err := installBridgeScriptsTo(ws, dir); err != nil {
		t.Fatal(err)
	}
	content2, _ := os.ReadFile(filepath.Join(dir, "xdg-open"))

	if string(content1) != string(content2) {
		t.Error("idempotent install produced different content")
	}
}
