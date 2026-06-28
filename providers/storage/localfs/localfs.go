// Package localfs is the M1 Storage provider: a persistent home per workspace at
// /home/dev. Two modes: a host directory (bind-mounted by docker/k8s) or a
// per-workspace ext4 image (a block device attached by the microVM backend,
// which can't pass through a host directory).
package localfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

const homeTarget = "/home/dev"

type Provider struct {
	root   string
	block  bool  // true: ext4-image homes (block device); false: directory homes
	sizeMB int64 // block-image size
}

func New(root string) *Provider { return &Provider{root: root} }

// NewBlock makes a Storage provider that hands out per-workspace ext4 images
// instead of directories — for backends that attach a block device (microVM).
// sizeMB is the home size; needs mkfs.ext4 on the host.
func NewBlock(root string, sizeMB int64) *Provider {
	return &Provider{root: root, block: true, sizeMB: sizeMB}
}

var _ ports.Storage = (*Provider)(nil)

func (p *Provider) EnsureHome(_ context.Context, r ports.HomeRequest) (ports.Mount, error) {
	if r.WorkspaceID == "" {
		return ports.Mount{}, fmt.Errorf("localfs: empty workspace id")
	}
	// workspace IDs are opaque tokens; reject anything that could escape root.
	if strings.ContainsAny(r.WorkspaceID, `/\`) || r.WorkspaceID == "." || r.WorkspaceID == ".." {
		return ports.Mount{}, fmt.Errorf("localfs: invalid workspace id %q", r.WorkspaceID)
	}
	if p.block {
		return p.ensureBlock(r.WorkspaceID)
	}
	dir := filepath.Join(p.root, r.WorkspaceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ports.Mount{}, fmt.Errorf("localfs: mkdir %s: %w", dir, err)
	}
	return ports.Mount{Source: dir, Target: homeTarget}, nil
}

// ensureBlock creates (once) a per-workspace ext4 image and returns it as a
// block-device mount. The image lives on the host independent of the box, so the
// home survives box destroy/recreate.
func (p *Provider) ensureBlock(wsID string) (ports.Mount, error) {
	if err := os.MkdirAll(p.root, 0o755); err != nil {
		return ports.Mount{}, fmt.Errorf("localfs: mkdir %s: %w", p.root, err)
	}
	img := filepath.Join(p.root, wsID+".ext4")
	if _, err := os.Stat(img); err != nil {
		f, err := os.Create(img)
		if err != nil {
			return ports.Mount{}, fmt.Errorf("localfs: create %s: %w", img, err)
		}
		err = f.Truncate(p.sizeMB << 20)
		f.Close()
		if err != nil {
			os.Remove(img)
			return ports.Mount{}, fmt.Errorf("localfs: size %s: %w", img, err)
		}
		if out, err := exec.Command("mkfs.ext4", "-q", "-F", img).CombinedOutput(); err != nil {
			os.Remove(img)
			return ports.Mount{}, fmt.Errorf("localfs: mkfs.ext4 %s: %v: %s", img, err, out)
		}
	}
	return ports.Mount{Source: img, Target: homeTarget, Device: true}, nil
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
