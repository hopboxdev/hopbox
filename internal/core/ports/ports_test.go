package ports_test

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

type fakeCompute struct{}

func (fakeCompute) Provision(ctx context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	return ports.Instance{Ref: "c-" + r.WorkspaceID, Phase: ports.InstanceRunning}, nil
}
func (fakeCompute) Status(ctx context.Context, ref string) (ports.Instance, error) {
	return ports.Instance{Ref: ref, Phase: ports.InstanceRunning}, nil
}
func (fakeCompute) Stop(ctx context.Context, ref string) error    { return nil }
func (fakeCompute) Destroy(ctx context.Context, ref string) error { return nil }

type fakeStorage struct{}

func (fakeStorage) EnsureHome(ctx context.Context, r ports.HomeRequest) (ports.Mount, error) {
	return ports.Mount{Source: "/data/" + r.WorkspaceID, Target: "/home/dev"}, nil
}
func (fakeStorage) Delete(ctx context.Context, homeRef string) error { return nil }

func TestPortsAreImplementable(t *testing.T) {
	var c ports.Compute = fakeCompute{}
	var s ports.Storage = fakeStorage{}

	inst, err := c.Provision(context.Background(), ports.ProvisionRequest{WorkspaceID: "w1"})
	if err != nil || inst.Ref != "c-w1" || inst.Phase != ports.InstanceRunning {
		t.Fatalf("provision: %+v err=%v", inst, err)
	}
	m, err := s.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1"})
	if err != nil || m.Source != "/data/w1" || m.Target != "/home/dev" {
		t.Fatalf("ensurehome: %+v err=%v", m, err)
	}
}
