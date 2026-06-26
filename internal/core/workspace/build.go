package workspace

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

// BuildFromSpec turns a parsed box Spec into a desired Workspace — the dev-env
// layer building on the box core. SSH-spawned boxes are temporary (krillbox
// semantics): always Ephemeral, with Grace from the parsed duration (0 = reap on
// disconnect). The backend is resolved against what is actually configured. It
// errors on special usernames, which spawn no box.
func BuildFromSpec(spec box.Spec, tenantID, owner, defaultImage string, backends []string, defBackend string) (*Workspace, error) {
	if spec.Special != "" {
		return nil, fmt.Errorf("special username %q spawns no workspace", spec.Special)
	}
	backend, err := box.ResolveBackend(spec.Backend, backends, defBackend)
	if err != nil {
		return nil, err
	}
	image := spec.Image
	if image == "" {
		image = defaultImage
	}
	w := New(tenantID, owner, spec.Name, image)
	w.Backend = backend
	w.Ephemeral = true
	w.Grace = spec.Grace
	return w, nil
}
