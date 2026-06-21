package conformance

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// RunBuildConformance exercises the ports.Build contract against a provider from
// factory. req is a build request the provider MUST resolve. Contract notes:
//   - Resolve MUST return an ImageRef with a non-empty Ref (the output is always
//     an OCI image reference).
//   - Status MUST return a non-empty Phase. The battery polls Status with the
//     async BuildRef when present, else with the resolved image Ref.
func RunBuildConformance(t *testing.T, factory func(t *testing.T) ports.Build, req ports.BuildRequest) {
	t.Helper()

	t.Run("ResolveReturnsImageRef", func(t *testing.T) {
		b := factory(t)
		img, err := b.Resolve(context.Background(), req)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if img.Ref == "" {
			t.Fatalf("resolved image ref must be non-empty: %+v", img)
		}
	})

	t.Run("StatusReturnsPhase", func(t *testing.T) {
		b := factory(t)
		img, err := b.Resolve(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		ref := img.BuildRef
		if ref == "" {
			ref = img.Ref
		}
		st, err := b.Status(context.Background(), ref)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.Phase == "" {
			t.Fatalf("status must report a phase: %+v", st)
		}
	})
}
