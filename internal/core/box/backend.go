package box

import (
	"errors"
	"fmt"
	"slices"
)

// ErrNoBackends means no compute backend is configured at all.
var ErrNoBackends = errors.New("no compute backends configured")

// ResolveBackend picks the compute backend for a box.
//
// It is the "auto" seam: a user never has to name a backend. An empty request
// means auto — deducible when there is exactly one backend, otherwise the
// configured default. An explicit request must name an available backend.
//
//   - requested != "": must be in available, else error.
//   - requested == "", one available: that one.
//   - requested == "", many available: def (must be available), else ambiguous.
func ResolveBackend(requested string, available []string, def string) (string, error) {
	if len(available) == 0 {
		return "", ErrNoBackends
	}
	has := func(b string) bool { return slices.Contains(available, b) }
	if requested != "" {
		if !has(requested) {
			return "", fmt.Errorf("backend %q not available (have %v)", requested, available)
		}
		return requested, nil
	}
	if len(available) == 1 {
		return available[0], nil
	}
	if def != "" {
		if !has(def) {
			return "", fmt.Errorf("default backend %q not available (have %v)", def, available)
		}
		return def, nil
	}
	return "", fmt.Errorf("multiple backends %v and none requested; specify one or set a default", available)
}
