package helper

import (
	"os"
	"path/filepath"
	"testing"
)

func writeHosts(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAddHostEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	writeHosts(t, path, "127.0.0.1 localhost\n")

	if err := AddHostEntry(path, "10.10.0.2", "mybox.hop"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestAddHostEntryIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	writeHosts(t, path, "127.0.0.1 localhost\n")

	if err := AddHostEntry(path, "10.10.0.2", "mybox.hop"); err != nil {
		t.Fatal(err)
	}
	if err := AddHostEntry(path, "10.10.0.2", "mybox.hop"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestAddMultipleHosts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	writeHosts(t, path, "127.0.0.1 localhost\n")

	if err := AddHostEntry(path, "10.10.0.2", "mybox.hop"); err != nil {
		t.Fatal(err)
	}
	if err := AddHostEntry(path, "10.10.0.3", "gaming.hop"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n10.10.0.3 gaming.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestRemoveHostEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	writeHosts(t, path, "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n# --- hopbox managed end ---\n")

	if err := RemoveHostEntry(path, "mybox.hop"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestRemoveOneOfMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	writeHosts(t, path, "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n10.10.0.3 gaming.hop\n# --- hopbox managed end ---\n")

	if err := RemoveHostEntry(path, "mybox.hop"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.3 gaming.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}
