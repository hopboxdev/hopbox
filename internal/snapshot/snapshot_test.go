package snapshot_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hopboxdev/hopbox/internal/snapshot"
)

// fakeRestic writes an executable shell script named "restic" in dir that
// records its arguments (one per line) to an .args file, prints stdout, and
// exits with exitCode. Returns the .args file path.
func fakeRestic(t *testing.T, dir, stdout string, exitCode int) string {
	t.Helper()
	argsFile := filepath.Join(dir, "restic.args")
	stdoutFile := filepath.Join(dir, "restic.stdout")
	if err := os.WriteFile(stdoutFile, []byte(stdout), 0644); err != nil {
		t.Fatalf("write stdout file: %v", err)
	}
	script := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %s\ncat %s\nexit %d\n",
		argsFile, stdoutFile, exitCode,
	)
	if err := os.WriteFile(filepath.Join(dir, "restic"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
	return argsFile
}

func readArgs(t *testing.T, argsFile string) string {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	parts := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return strings.Join(parts, " ")
}

func TestCreate_EmptyPaths(t *testing.T) {
	_, err := snapshot.Create(context.Background(), "s3:example/repo", nil, nil)
	if err == nil {
		t.Error("expected error for empty paths")
	}
}

func TestCreate_ParsesSummaryID(t *testing.T) {
	dir := t.TempDir()
	// restic --json backup outputs one JSON object per line; the last line is the summary.
	backupOutput := `{"message_type":"status","percent_done":0.5}
{"message_type":"summary","files_new":3,"snapshot_id":"abc12345def67890"}
`
	af := fakeRestic(t, dir, backupOutput, 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	result, err := snapshot.Create(context.Background(), "local:/tmp/repo", []string{"/home/user"}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.SnapshotID != "abc12345def67890" {
		t.Errorf("SnapshotID = %q, want %q", result.SnapshotID, "abc12345def67890")
	}
	// Verify restic was called with the correct subcommand and paths.
	got := readArgs(t, af)
	if !strings.HasPrefix(got, "backup --json") {
		t.Errorf("restic args = %q, want prefix %q", got, "backup --json")
	}
	if !strings.Contains(got, "/home/user") {
		t.Errorf("restic args = %q, missing path /home/user", got)
	}
}

func TestCreate_ResticError(t *testing.T) {
	dir := t.TempDir()
	fakeRestic(t, dir, "fatal: repository not found\n", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	_, err := snapshot.Create(context.Background(), "local:/tmp/repo", []string{"/home"}, nil)
	if err == nil {
		t.Error("expected error when restic exits non-zero")
	}
}

func TestList_ParsesJSON(t *testing.T) {
	dir := t.TempDir()
	listOutput := `[{"id":"abc123456789abcd","short_id":"abc12345","time":"2024-01-15T10:00:00Z","paths":["/home/user"],"hostname":"myhost","tags":["daily"]}]
`
	fakeRestic(t, dir, listOutput, 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	snaps, err := snapshot.List(context.Background(), "local:/tmp/repo", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	s := snaps[0]
	if s.ID != "abc123456789abcd" {
		t.Errorf("ID = %q, want %q", s.ID, "abc123456789abcd")
	}
	if s.ShortID != "abc12345" {
		t.Errorf("ShortID = %q, want %q", s.ShortID, "abc12345")
	}
	if s.Hostname != "myhost" {
		t.Errorf("Hostname = %q, want %q", s.Hostname, "myhost")
	}
	if len(s.Paths) != 1 || s.Paths[0] != "/home/user" {
		t.Errorf("Paths = %v, want [/home/user]", s.Paths)
	}
}

func TestList_ResticError(t *testing.T) {
	dir := t.TempDir()
	fakeRestic(t, dir, "", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	_, err := snapshot.List(context.Background(), "local:/tmp/repo", nil)
	if err == nil {
		t.Error("expected error when restic exits non-zero")
	}
}

func TestRestore_DefaultTarget(t *testing.T) {
	dir := t.TempDir()
	af := fakeRestic(t, dir, "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	// Empty restoreTo should default to "/".
	if err := snapshot.Restore(context.Background(), "local:/tmp/repo", "abc123", "", nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got := readArgs(t, af)
	if !strings.Contains(got, "--target /") {
		t.Errorf("restic args = %q, missing --target /", got)
	}
}

func TestRestore_CustomTarget(t *testing.T) {
	dir := t.TempDir()
	af := fakeRestic(t, dir, "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := snapshot.Restore(context.Background(), "local:/tmp/repo", "abc123", "/restore/here", nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got := readArgs(t, af)
	if !strings.Contains(got, "--target /restore/here") {
		t.Errorf("restic args = %q, missing --target /restore/here", got)
	}
	if !strings.Contains(got, "abc123") {
		t.Errorf("restic args = %q, missing snapshot id abc123", got)
	}
}

func TestRestore_ResticError(t *testing.T) {
	dir := t.TempDir()
	fakeRestic(t, dir, "", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := snapshot.Restore(context.Background(), "local:/tmp/repo", "abc123", "/", nil); err == nil {
		t.Error("expected error when restic exits non-zero")
	}
}
