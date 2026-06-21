package server_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/hopboxdev/hopbox/gen/hopbox/provider/v1"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/plugin/server"
)

type fakeStorage struct{ deleted string }

func (f *fakeStorage) EnsureHome(_ context.Context, r ports.HomeRequest) (ports.Mount, error) {
	return ports.Mount{Source: "vol-" + r.WorkspaceID, Target: "/home/dev"}, nil
}
func (f *fakeStorage) Delete(_ context.Context, ref string) error { f.deleted = ref; return nil }

func TestStorageServerServesEnsureHome(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterStorageServer(gs, server.NewStorage(&fakeStorage{}))
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cli := pb.NewStorageClient(conn)

	m, err := cli.EnsureHome(context.Background(), &pb.HomeRequest{WorkspaceId: "w1"})
	if err != nil {
		t.Fatalf("ensurehome: %v", err)
	}
	if m.Source != "vol-w1" || m.Target != "/home/dev" {
		t.Fatalf("bad mount: %+v", m)
	}
}
