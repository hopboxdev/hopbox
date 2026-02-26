package packages

import (
	"context"
	"encoding/json"
	"fmt"
)

// BackendType identifies a package backend.
type BackendType int

const (
	// Apt is the default package backend (Debian/Ubuntu apt-get).
	Apt BackendType = iota
	// Nix uses the Nix package manager.
	Nix
	// Static downloads a binary from a URL.
	Static
)

var backendNames = map[BackendType]string{
	Apt:    "apt",
	Nix:    "nix",
	Static: "static",
}

var backendValues = map[string]BackendType{
	"":       Apt,
	"apt":    Apt,
	"nix":    Nix,
	"static": Static,
}

func (b BackendType) String() string {
	if s, ok := backendNames[b]; ok {
		return s
	}
	return fmt.Sprintf("BackendType(%d)", b)
}

// ParseBackendType parses a string into a BackendType.
// An empty string defaults to Apt.
func ParseBackendType(s string) (BackendType, error) {
	if b, ok := backendValues[s]; ok {
		return b, nil
	}
	return 0, fmt.Errorf("unknown package backend %q", s)
}

// MarshalJSON serializes BackendType as a string for backward-compatible JSON.
func (b BackendType) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}

// UnmarshalJSON deserializes BackendType from a JSON string.
func (b *BackendType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseBackendType(s)
	if err != nil {
		return err
	}
	*b = parsed
	return nil
}

// Backend is the interface every package backend implements.
type Backend interface {
	Install(ctx context.Context, pkg Package) error
	IsInstalled(ctx context.Context, pkg Package) (bool, error)
	Remove(ctx context.Context, pkg Package) error
}

// backends is the registry of all available backends.
var backends = map[BackendType]Backend{
	Apt:    aptBackend{},
	Nix:    nixBackend{},
	Static: staticBackend{},
}

// lookupBackend returns the backend for the given type.
func lookupBackend(t BackendType) (Backend, error) {
	b, ok := backends[t]
	if !ok {
		return nil, fmt.Errorf("unknown package backend %q", t)
	}
	return b, nil
}
