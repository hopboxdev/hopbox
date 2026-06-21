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
	"github.com/mesadev/mesa/providers/metering/staticquota"
	"github.com/mesadev/mesa/test/conformance"
)

func TestStaticQuotaInproc(t *testing.T) {
	conformance.RunMeteringConformance(t, func(t *testing.T) ports.Metering {
		return staticquota.New(5)
	})
}

func TestStaticQuotaOverRemote(t *testing.T) {
	conformance.RunMeteringConformance(t, func(t *testing.T) ports.Metering {
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterMeteringServer(gs, server.NewMetering(staticquota.New(5)))
		go func() { _ = gs.Serve(lis) }()
		t.Cleanup(gs.Stop)
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return plugin.NewRemoteMetering(conn)
	})
}
