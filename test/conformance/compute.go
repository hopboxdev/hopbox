package conformance

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
)

// RunComputeConformance exercises the ports.Compute lifecycle contract against
// a provider from factory.
func RunComputeConformance(t *testing.T, factory func(t *testing.T) ports.Compute, req ports.ProvisionRequest) {
	t.Helper()

	t.Run("ProvisionReturnsRunningRef", func(t *testing.T) {
		c := factory(t)
		inst, err := c.Provision(context.Background(), req)
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		if inst.Ref == "" {
			t.Fatalf("instance ref empty: %+v", inst)
		}
		t.Cleanup(func() { _ = c.Destroy(context.Background(), inst.Ref) })
	})

	t.Run("StatusAfterProvision", func(t *testing.T) {
		c := factory(t)
		inst, err := c.Provision(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Destroy(context.Background(), inst.Ref) })
		st, err := c.Status(context.Background(), inst.Ref)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.Phase != ports.InstanceRunning && st.Phase != ports.InstanceStopped {
			t.Fatalf("unexpected phase after provision: %s", st.Phase)
		}
	})

	t.Run("DestroyThenGone", func(t *testing.T) {
		c := factory(t)
		inst, err := c.Provision(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Destroy(context.Background(), inst.Ref); err != nil {
			t.Fatalf("destroy: %v", err)
		}
		st, err := c.Status(context.Background(), inst.Ref)
		if err != nil {
			t.Fatalf("status after destroy: %v", err)
		}
		if st.Phase != ports.InstanceGone {
			t.Fatalf("phase after destroy = %s, want gone", st.Phase)
		}
	})
}
