// Package prebuilt is the identity Build provider: it resolves a source that is
// already an OCI image reference straight through, unchanged ("bring your own
// image"). There is no build step, so resolution is synchronous and the image
// is always ready. It is the zero-dependency reference Build provider; the
// devcontainer provider (real builds from a devcontainer.json) is the MVP
// provider, built against this same contract when live build infra is wired.
package prebuilt

import (
	"context"
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

type Provider struct{}

var _ ports.Build = (*Provider)(nil)

func New() *Provider { return &Provider{} }

// Resolve returns the source ref as the image ref, unchanged. BuildRef is empty
// (resolution is synchronous — nothing to poll).
func (p *Provider) Resolve(_ context.Context, r ports.BuildRequest) (ports.ImageRef, error) {
	if r.SourceRef == "" {
		return ports.ImageRef{}, fmt.Errorf("prebuilt: source_ref is required (it is the image ref)")
	}
	return ports.ImageRef{Ref: r.SourceRef}, nil
}

// Status reports a prebuilt image as ready: buildRef is the image ref itself.
func (p *Provider) Status(_ context.Context, buildRef string) (ports.BuildStatus, error) {
	return ports.BuildStatus{Phase: "ready", ImageRef: buildRef}, nil
}
