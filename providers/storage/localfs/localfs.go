// Package localfs is the M1 Storage provider: one host directory per workspace,
// bind-mounted to /home/dev. Mirrors hopbox's bind-mounted homes.
package localfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mesadev/mesa/internal/core/ports"
)

const homeTarget = "/home/dev"

type Provider struct{ root string }

func New(root string) *Provider { return &Provider{root: root} }

var _ ports.Storage = (*Provider)(nil)

func (p *Provider) EnsureHome(_ context.Context, r ports.HomeRequest) (ports.Mount, error) {
	if r.WorkspaceID == "" {
		return ports.Mount{}, fmt.Errorf("localfs: empty workspace id")
	}
	// workspace IDs are opaque tokens; reject anything that could escape root.
	if strings.ContainsAny(r.WorkspaceID, `/\`) || r.WorkspaceID == "." || r.WorkspaceID == ".." {
		return ports.Mount{}, fmt.Errorf("localfs: invalid workspace id %q", r.WorkspaceID)
	}
	dir := filepath.Join(p.root, r.WorkspaceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ports.Mount{}, fmt.Errorf("localfs: mkdir %s: %w", dir, err)
	}
	return ports.Mount{Source: dir, Target: homeTarget}, nil
}

// Delete removes a workspace home. homeRef must be the absolute Mount.Source
// returned by EnsureHome; anything outside the provider root is refused.
func (p *Provider) Delete(_ context.Context, homeRef string) error {
	// safety: never delete outside our root
	if !strings.HasPrefix(filepath.Clean(homeRef), filepath.Clean(p.root)+string(os.PathSeparator)) {
		return fmt.Errorf("localfs: refusing to delete %q outside root %q", homeRef, p.root)
	}
	return os.RemoveAll(homeRef)
}
