package service

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ListeningPort describes a TCP port that is currently listening.
type ListeningPort struct {
	Port    int    `json:"port"`
	Program string `json:"program,omitempty"`
}

// ListeningPorts returns all TCP ports currently in LISTEN state on Linux.
// On non-Linux platforms it returns an empty slice.
func ListeningPorts() ([]ListeningPort, error) {
	return ListFromProcNetTCP("/proc/net/tcp")
}

// ListFromProcNetTCP parses a /proc/net/tcp-format file and returns listening
// ports. Exported for testing with synthetic files.
func ListFromProcNetTCP(path string) ([]ListeningPort, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // non-Linux
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var ports []ListeningPort
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 4 {
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
		portHex := parts[1]
		portNum, err := strconv.ParseInt(portHex, 16, 32)
		if err != nil {
			continue
		}
		ports = append(ports, ListeningPort{Port: int(portNum)})
	}
	return ports, nil
}
