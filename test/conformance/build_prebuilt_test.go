package conformance_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/mesadev/mesa/gen/mesa/provider/v1"
	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/plugin"
	"github.com/mesadev/mesa/internal/plugin/server"
	"github.com/mesadev/mesa/providers/build/prebuilt"
	"github.com/mesadev/mesa/test/conformance"
)

func buildReq() ports.BuildRequest {
	return ports.BuildRequest{WorkspaceID: "w1", SourceRef: "ubuntu:24.04", Provider: "prebuilt", TenantID: "default"}
}

func TestPrebuiltInproc(t *testing.T) {
	conformance.RunBuildConformance(t, func(t *testing.T) ports.Build {
		return prebuilt.New()
	}, buildReq())
}

func TestPrebuiltOverRemote(t *testing.T) {
	conformance.RunBuildConformance(t, func(t *testing.T) ports.Build {
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterBuildServer(gs, server.NewBuild(prebuilt.New()))
		go func() { _ = gs.Serve(lis) }()
		t.Cleanup(gs.Stop)
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return plugin.NewRemoteBuild(conn)
	}, buildReq())
}
