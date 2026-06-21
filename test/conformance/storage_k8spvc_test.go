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
	k8sstorage "github.com/hopboxdev/hopbox/providers/storage/kubernetes"
	"github.com/hopboxdev/hopbox/test/conformance"
)

func newK8sStorage(t *testing.T) ports.Storage {
	t.Helper()
	p, err := k8sstorage.New(fake.NewSimpleClientset(), "hopbox-workspaces", "", "1Gi")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestK8sPVCInproc(t *testing.T) {
	conformance.RunStorageConformance(t, func(t *testing.T) ports.Storage { return newK8sStorage(t) })
}

func TestK8sPVCOverRemote(t *testing.T) {
	conformance.RunStorageConformance(t, func(t *testing.T) ports.Storage {
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterStorageServer(gs, server.NewStorage(newK8sStorage(t)))
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
