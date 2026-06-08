package conformance_test

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/mesadev/mesa/gen/mesa/provider/v1"
	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/plugin"
	"github.com/mesadev/mesa/internal/plugin/server"
	"github.com/mesadev/mesa/test/conformance"
)

// memCompute is a minimal in-memory ports.Compute used to validate the battery
// itself and to serve as a reference for provider authors.
type memCompute struct {
	mu   sync.Mutex
	live map[string]bool
}

func newMemCompute() *memCompute { return &memCompute{live: map[string]bool{}} }

func (m *memCompute) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref := "mem-" + r.WorkspaceID
	m.live[ref] = true
	return ports.Instance{Ref: ref, Phase: ports.InstanceRunning}, nil
}
func (m *memCompute) Status(_ context.Context, ref string) (ports.Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.live[ref] {
		return ports.Instance{Ref: ref, Phase: ports.InstanceRunning}, nil
	}
	return ports.Instance{Ref: ref, Phase: ports.InstanceGone}, nil
}
func (m *memCompute) Stop(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.live, ref)
	return nil
}
func (m *memCompute) Destroy(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.live, ref)
	return nil
}

func dialMemCompute(t *testing.T, impl ports.Compute) ports.Compute {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterComputeServer(gs, server.NewCompute(impl))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return plugin.NewRemoteCompute(conn)
}

func TestComputeFakeInproc(t *testing.T) {
	conformance.RunComputeConformance(t, func(t *testing.T) ports.Compute {
		return newMemCompute()
	}, ports.ProvisionRequest{WorkspaceID: "w1", ImageRef: "ubuntu:24.04"})
}

func TestComputeFakeOverRemote(t *testing.T) {
	conformance.RunComputeConformance(t, func(t *testing.T) ports.Compute {
		return dialMemCompute(t, newMemCompute())
	}, ports.ProvisionRequest{WorkspaceID: "w1", ImageRef: "ubuntu:24.04"})
}
