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
	"github.com/mesadev/mesa/providers/ingress/subdomain"
	"github.com/mesadev/mesa/test/conformance"
)

func TestSubdomainInproc(t *testing.T) {
	conformance.RunIngressConformance(t, func(t *testing.T) ports.Ingress {
		return subdomain.New("gw.example.com")
	})
}

func TestSubdomainOverRemote(t *testing.T) {
	conformance.RunIngressConformance(t, func(t *testing.T) ports.Ingress {
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterIngressServer(gs, server.NewIngress(subdomain.New("gw.example.com")))
		go func() { _ = gs.Serve(lis) }()
		t.Cleanup(gs.Stop)
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return plugin.NewRemoteIngress(conn)
	})
}
