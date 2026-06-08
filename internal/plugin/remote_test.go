package plugin_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/mesadev/mesa/gen/mesa/provider/v1"
	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/plugin"
	"github.com/mesadev/mesa/internal/plugin/server"
)

type fakeCompute struct {
	lastReq ports.ProvisionRequest
	err     error
}

func (f *fakeCompute) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	f.lastReq = r
	if f.err != nil {
		return ports.Instance{}, f.err
	}
	return ports.Instance{Ref: "c-" + r.WorkspaceID, Phase: ports.InstanceRunning}, nil
}
func (f *fakeCompute) Status(_ context.Context, ref string) (ports.Instance, error) {
	return ports.Instance{Ref: ref, Phase: ports.InstanceRunning}, nil
}
func (f *fakeCompute) Stop(context.Context, string) error    { return nil }
func (f *fakeCompute) Destroy(context.Context, string) error { return nil }

func dialCompute(t *testing.T, impl ports.Compute) (ports.Compute, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterComputeServer(gs, server.NewCompute(impl))
	go func() { _ = gs.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return plugin.NewRemoteCompute(conn), func() { _ = conn.Close(); gs.Stop() }
}

func TestRemoteComputeRoundTrip(t *testing.T) {
	fake := &fakeCompute{}
	rc, done := dialCompute(t, fake)
	defer done()

	inst, err := rc.Provision(context.Background(), ports.ProvisionRequest{
		WorkspaceID: "w1", ImageRef: "ubuntu:24.04",
		Agent: ports.AgentImage{ImageRef: "img:1", BinaryPath: "/a", TargetPath: "/mesa/mesa-agent"},
	})
	if err != nil || inst.Ref != "c-w1" || inst.Phase != ports.InstanceRunning {
		t.Fatalf("provision: %+v err=%v", inst, err)
	}
	if fake.lastReq.Agent.ImageRef != "img:1" || fake.lastReq.Agent.TargetPath != "/mesa/mesa-agent" {
		t.Fatalf("agent not transported: %+v", fake.lastReq.Agent)
	}
	st, err := rc.Status(context.Background(), "c-w1")
	if err != nil || st.Phase != ports.InstanceRunning {
		t.Fatalf("status: %+v err=%v", st, err)
	}
}

func TestRemoteComputePropagatesError(t *testing.T) {
	fake := &fakeCompute{err: errors.New("boom from provider")}
	rc, done := dialCompute(t, fake)
	defer done()
	_, err := rc.Provision(context.Background(), ports.ProvisionRequest{WorkspaceID: "w1"})
	if err == nil {
		t.Fatal("expected error to propagate across the gRPC boundary, got nil")
	}
	if !strings.Contains(err.Error(), "boom from provider") {
		t.Fatalf("error message lost across boundary: %v", err)
	}
}

type fakeStorage struct{ deleted string }

func (f *fakeStorage) EnsureHome(_ context.Context, r ports.HomeRequest) (ports.Mount, error) {
	return ports.Mount{Source: "vol-" + r.WorkspaceID, Target: "/home/dev", ReadOnly: true}, nil
}
func (f *fakeStorage) Delete(_ context.Context, ref string) error { f.deleted = ref; return nil }

func dialStorage(t *testing.T, impl ports.Storage) (ports.Storage, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterStorageServer(gs, server.NewStorage(impl))
	go func() { _ = gs.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return plugin.NewRemoteStorage(conn), func() { _ = conn.Close(); gs.Stop() }
}

func TestRemoteStorageRoundTrip(t *testing.T) {
	rs, done := dialStorage(t, &fakeStorage{})
	defer done()
	m, err := rs.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1", TenantID: "default", Owner: "alice"})
	if err != nil {
		t.Fatalf("ensurehome: %v", err)
	}
	if m.Source != "vol-w1" || m.Target != "/home/dev" || !m.ReadOnly {
		t.Fatalf("mount not transported intact: %+v", m)
	}
	if err := rs.Delete(context.Background(), "vol-w1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
