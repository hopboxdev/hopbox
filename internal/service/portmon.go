package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ListeningPort describes a TCP port that is currently listening.
type ListeningPort struct {
	Port    int    `json:"port"`
	Program string `json:"program,omitempty"`
}

// listenEntry is an intermediate result from parsing /proc/net/tcp.
type listenEntry struct {
	port  int
	inode int
}

// ListeningPorts returns all TCP ports currently in LISTEN state on Linux,
// with program names resolved from /proc. On non-Linux platforms it returns
// an empty slice.
func ListeningPorts() ([]ListeningPort, error) {
	return ListFromProcRoot("/proc")
}

// ListFromProcRoot parses /proc/net/tcp and resolves program names using the
// given proc root. Exported for testing with synthetic /proc trees.
func ListFromProcRoot(procRoot string) ([]ListeningPort, error) {
	entries, err := parseProcNetTCP(filepath.Join(procRoot, "net/tcp"))
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return resolvePrograms(procRoot, entries), nil
}

// ListFromProcNetTCP parses a /proc/net/tcp-format file and returns listening
// ports. Exported for testing with synthetic files. Program names are not
// resolved — use ListeningPorts() for full resolution.
func ListFromProcNetTCP(path string) ([]ListeningPort, error) {
	entries, err := parseProcNetTCP(path)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	ports := make([]ListeningPort, len(entries))
	for i, e := range entries {
		ports[i] = ListeningPort{Port: e.port}
	}
	return ports, nil
}

// parseProcNetTCP parses a /proc/net/tcp-format file and returns listen entries
// with port and inode.
func parseProcNetTCP(path string) ([]listenEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // non-Linux
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var entries []listenEntry
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		// State is fields[3]. 0A = LISTEN
		if fields[3] != "0A" {
			continue
		}
		// Local address is fields[1]: "hex_ip:hex_port"
		parts := strings.SplitN(fields[1], ":", 2)
		if len(parts) != 2 {
			continue
		}
		portNum, err := strconv.ParseInt(parts[1], 16, 32)
		if err != nil {
			continue
		}
		inode, err := strconv.Atoi(fields[9])
		if err != nil {
			continue
		}
		entries = append(entries, listenEntry{port: int(portNum), inode: inode})
	}
	return entries, nil
}

// resolvePrograms maps socket inodes to process names by scanning /proc/<pid>/fd/
// and /proc/<pid>/cmdline — the same approach ss -tlnp uses.
func resolvePrograms(procRoot string, entries []listenEntry) []ListeningPort {
	// Build inode → index map for quick lookup.
	inodeIdx := make(map[int][]int, len(entries))
	for i, e := range entries {
		inodeIdx[e.inode] = append(inodeIdx[e.inode], i)
	}

	// Pre-allocate results.
	ports := make([]ListeningPort, len(entries))
	for i, e := range entries {
		ports[i] = ListeningPort{Port: e.port}
	}

	// Scan /proc for numeric PID directories.
	procEntries, err := os.ReadDir(procRoot)
	if err != nil {
		return ports
	}

	for _, de := range procEntries {
		if !de.IsDir() {
			continue
		}
		// Only numeric directory names (PIDs).
		if _, err := strconv.Atoi(de.Name()); err != nil {
			continue
		}
		if len(inodeIdx) == 0 {
			break // all resolved
		}
		pidDir := filepath.Join(procRoot, de.Name())
		resolveForPID(pidDir, inodeIdx, ports)
	}

	return ports
}

// resolveForPID checks one PID's fd/ symlinks against the inode set.
func resolveForPID(pidDir string, inodeIdx map[int][]int, ports []ListeningPort) {
	fdDir := filepath.Join(pidDir, "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return // permission denied or process exited
	}

	var program string // lazily resolved from cmdline

	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue
		}
		// Socket links look like "socket:[12345]"
		if !strings.HasPrefix(link, "socket:[") || !strings.HasSuffix(link, "]") {
			continue
		}
		inodeStr := link[len("socket:[") : len(link)-1]
		inode, err := strconv.Atoi(inodeStr)
		if err != nil {
			continue
		}
		indices, ok := inodeIdx[inode]
		if !ok {
			continue
		}

		// Lazily read the program name.
		if program == "" {
			program = readProgram(filepath.Join(pidDir, "cmdline"))
			if program == "" {
				program = "?" // fallback
			}
		}

		for _, idx := range indices {
			ports[idx].Program = program
		}
		delete(inodeIdx, inode)
	}
}

// readProgram reads /proc/<pid>/cmdline and returns the base name of the first
// argument. Returns "" on error.
func readProgram(cmdlinePath string) string {
	data, err := os.ReadFile(cmdlinePath)
	if err != nil || len(data) == 0 {
		return ""
	}
	// cmdline is null-byte separated; take first arg.
	if idx := strings.IndexByte(string(data), 0); idx > 0 {
		data = data[:idx]
	}
	return filepath.Base(string(data))
}
