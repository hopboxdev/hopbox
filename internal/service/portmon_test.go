package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/service"
)

// TestListeningPorts_Real tests ListeningPorts on the current OS.
// On non-Linux it returns an empty slice (not an error).
func TestListeningPorts_Real(t *testing.T) {
	ports, err := service.ListeningPorts()
	if err != nil {
		t.Fatalf("ListeningPorts: %v", err)
	}
	// Result is a slice — may be empty on macOS, non-empty on Linux.
	for _, p := range ports {
		if p.Port <= 0 || p.Port > 65535 {
			t.Errorf("invalid port %d", p.Port)
		}
	}
}

// TestListFromProcNetTCP_Parsing tests the /proc/net/tcp parser with a
// synthetic file, independent of the actual OS.
func TestListFromProcNetTCP_Parsing(t *testing.T) {
	// Synthetic /proc/net/tcp content. State 0A = LISTEN.
	// Local address format: hex_ip:hex_port
	// Port 0x1F90 = 8080, port 0x0050 = 80, state 01 = ESTABLISHED (not LISTEN)
	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12346 1 0000000000000000 100 0 0 10 0
   2: 0F02000A:8E43 0101010A:0035 01 00000000:00000000 00:00000000 00000000     0        0 12347 1 0000000000000000 100 0 0 10 0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "tcp")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ports, err := service.ListFromProcNetTCP(path)
	if err != nil {
		t.Fatalf("ListFromProcNetTCP: %v", err)
	}
	if len(ports) != 2 {
		t.Fatalf("got %d LISTEN ports, want 2", len(ports))
	}

	portSet := map[int]bool{}
	for _, p := range ports {
		portSet[p.Port] = true
	}
	if !portSet[8080] {
		t.Error("expected port 8080 (0x1F90)")
	}
	if !portSet[80] {
		t.Error("expected port 80 (0x0050)")
	}
	if portSet[36419] { // 0x8E43 = ESTABLISHED, should be excluded
		t.Error("should not include non-LISTEN port 36419")
	}
}

func TestListFromProcNetTCP_NotExist(t *testing.T) {
	ports, err := service.ListFromProcNetTCP("/nonexistent/path/tcp")
	if err != nil {
		t.Fatalf("expected nil error for non-existent path, got: %v", err)
	}
	if ports != nil {
		t.Errorf("expected nil ports for non-existent path, got %v", ports)
	}
}

func TestListFromProcNetTCP_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tcp")
	if err := os.WriteFile(path, []byte("  sl  local_address rem_address   st\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ports, err := service.ListFromProcNetTCP(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 {
		t.Errorf("expected 0 ports, got %d", len(ports))
	}
}
