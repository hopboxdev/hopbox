package packages

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// StatePath is the default location for the installed-packages state file.
// Variable so tests can override it.
var StatePath = "/etc/hopbox/installed-packages.json"

// stateFile is the JSON envelope for the state file.
type stateFile struct {
	Packages []Package `json:"packages"`
}

// LoadState reads the installed-packages state file.
// Returns an empty slice (not an error) if the file does not exist.
func LoadState(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	return sf.Packages, nil
}

// SaveState writes the package list to the state file atomically.
func SaveState(path string, pkgs []Package) error {
	sf := stateFile{Packages: pkgs}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
