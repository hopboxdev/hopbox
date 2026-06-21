//go:build k8s

package conformance_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"k8s.io/client-go/kubernetes/fake"

	pb "github.com/hopboxdev/hopbox/gen/hopbox/provider/v1"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/plugin"
	"github.com/hopboxdev/hopbox/internal/plugin/server"
	k8scompute "github.com/hopboxdev/hopbox/providers/compute/kubernetes"
	"github.com/hopboxdev/hopbox/test/conformance"
)

func k8sComputeReq() ports.ProvisionRequest {
	return ports.ProvisionRequest{
		WorkspaceID: "conf1",
		ImageRef:    "ubuntu:24.04",
		Agent:       ports.AgentImage{ImageRef: "ghcr.io/hopboxdev/hopbox-agent:0.2.0", BinaryPath: "/hopbox-agent", TargetPath: "/hopbox/hopbox-agent"},
		Env:         map[string]string{"HOPBOX_WORKSPACE_ID": "conf1"},
	}
}

func TestK8sComputeInproc(t *testing.T) {
	conformance.RunComputeConformance(t, func(t *testing.T) ports.Compute {
		return k8scompute.New(fake.NewSimpleClientset(), "hopbox-workspaces")
	}, k8sComputeReq())
}

func TestK8sComputeOverRemote(t *testing.T) {
	conformance.RunComputeConformance(t, func(t *testing.T) ports.Compute {
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterComputeServer(gs, server.NewCompute(k8scompute.New(fake.NewSimpleClientset(), "hopbox-workspaces")))
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
	}, k8sComputeReq())
}
