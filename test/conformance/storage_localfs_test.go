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
	"github.com/mesadev/mesa/providers/storage/localfs"
	"github.com/mesadev/mesa/test/conformance"
)

func TestLocalfsInproc(t *testing.T) {
	conformance.RunStorageConformance(t, func(t *testing.T) ports.Storage {
		return localfs.New(t.TempDir())
	})
}

func TestLocalfsOverRemote(t *testing.T) {
	conformance.RunStorageConformance(t, func(t *testing.T) ports.Storage {
		impl := localfs.New(t.TempDir())
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterStorageServer(gs, server.NewStorage(impl))
		go func() { _ = gs.Serve(lis) }()
		t.Cleanup(gs.Stop)
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return plugin.NewRemoteStorage(conn)
	})
}
