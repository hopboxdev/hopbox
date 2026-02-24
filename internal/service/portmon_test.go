package service_test

import (
	"fmt"
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
	// Port 0x1F90 = 8080 (inode 12345), port 0x0050 = 80 (inode 12346),
	// state 01 = ESTABLISHED (not LISTEN, inode 12347)
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

// TestResolvePrograms tests inode→program resolution using a synthetic /proc tree.
func TestResolvePrograms(t *testing.T) {
	// Create a synthetic /proc tree:
	//   procRoot/
	//     net/tcp          — two LISTEN entries with inodes 1001 and 1002
	//     42/              — PID 42
	//       fd/
	//         3 -> socket:[1001]
	//       cmdline        — "nginx\x00-g\x00daemon off;\x00"
	//     99/              — PID 99
	//       fd/
	//         5 -> socket:[1002]
	//       cmdline        — "/usr/bin/node\x00server.js\x00"

	procRoot := t.TempDir()

	// /proc/net/tcp
	netDir := filepath.Join(procRoot, "net")
	if err := os.MkdirAll(netDir, 0755); err != nil {
		t.Fatal(err)
	}
	tcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1001 1 0000000000000000 100 0 0 10 0
   1: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1002 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	// PID 42: nginx on port 8080 (inode 1001)
	mkProcPID(t, procRoot, "42", "1001", "nginx\x00-g\x00daemon off;\x00")

	// PID 99: node on port 3000 (inode 1002)
	mkProcPID(t, procRoot, "99", "1002", "/usr/bin/node\x00server.js\x00")

	ports, err := service.ListFromProcRoot(procRoot)
	if err != nil {
		t.Fatalf("ListFromProcRoot: %v", err)
	}
	if len(ports) != 2 {
		t.Fatalf("got %d ports, want 2", len(ports))
	}

	byPort := map[int]string{}
	for _, p := range ports {
		byPort[p.Port] = p.Program
	}

	if byPort[8080] != "nginx" {
		t.Errorf("port 8080 program = %q, want %q", byPort[8080], "nginx")
	}
	if byPort[3000] != "node" {
		t.Errorf("port 3000 program = %q, want %q", byPort[3000], "node")
	}
}

// TestResolvePrograms_Unresolved verifies that ports with no matching PID
// get an empty program name.
func TestResolvePrograms_Unresolved(t *testing.T) {
	procRoot := t.TempDir()

	netDir := filepath.Join(procRoot, "net")
	if err := os.MkdirAll(netDir, 0755); err != nil {
		t.Fatal(err)
	}
	tcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 9999 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	ports, err := service.ListFromProcRoot(procRoot)
	if err != nil {
		t.Fatalf("ListFromProcRoot: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("got %d ports, want 1", len(ports))
	}
	if ports[0].Port != 8080 {
		t.Errorf("port = %d, want 8080", ports[0].Port)
	}
	if ports[0].Program != "" {
		t.Errorf("program = %q, want empty (unresolved)", ports[0].Program)
	}
}

// mkProcPID creates a synthetic /proc/<pid>/ with fd/ symlinks and cmdline.
func mkProcPID(t *testing.T, procRoot, pid, inode, cmdline string) {
	t.Helper()
	pidDir := filepath.Join(procRoot, pid)
	fdDir := filepath.Join(pidDir, "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create symlink: fd/3 -> socket:[INODE]
	if err := os.Symlink(fmt.Sprintf("socket:[%s]", inode), filepath.Join(fdDir, "3")); err != nil {
		t.Fatal(err)
	}
	// Write cmdline
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte(cmdline), 0644); err != nil {
		t.Fatal(err)
	}
}
