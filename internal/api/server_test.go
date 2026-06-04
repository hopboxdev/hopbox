package api_test

import (
	"context"
	"io"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
	"github.com/mesadev/mesa/internal/agentproto"
	"github.com/mesadev/mesa/internal/api"
	"github.com/mesadev/mesa/internal/core/store/sqlite"
)

// fakeHub returns a pre-baked pipe whose far end echoes input back, so the
// Shell bridge can be tested without a real agent.
type fakeHub struct{ connected bool }

func (f *fakeHub) Connected(string) bool { return f.connected }
func (f *fakeHub) OpenShell(_ context.Context, _ string, _ agentproto.ShellHeader) (io.ReadWriteCloser, error) {
	c1, c2 := net.Pipe()
	// Echo server on the far ("agent") end: bytes the bridge writes to c1 are
	// read from c2 and written straight back, so they surface on c1's reader.
	go func() { _, _ = io.Copy(c2, c2) }()
	return c1, nil
}

func dialer(t *testing.T) (mesav1.WorkspaceServiceClient, func()) {
	t.Helper()
	s, err := sqlite.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(s, &fakeHub{connected: true}, "default", "alice")

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	mesav1.RegisterWorkspaceServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return mesav1.NewWorkspaceServiceClient(conn), func() {
		_ = conn.Close()
		gs.Stop()
		_ = s.Close()
	}
}

func TestCreateGetListDelete(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()

	w, err := c.CreateWorkspace(ctx, &mesav1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Phase != "Pending" || w.Name != "proj" {
		t.Fatalf("bad create: %+v", w)
	}
	got, err := c.GetWorkspace(ctx, &mesav1.GetWorkspaceRequest{NameOrId: "proj"})
	if err != nil || got.Id != w.Id {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	list, err := c.ListWorkspaces(ctx, &mesav1.ListWorkspacesRequest{})
	if err != nil || len(list.Workspaces) != 1 {
		t.Fatalf("list: %d err=%v", len(list.Workspaces), err)
	}
	if _, err := c.DeleteWorkspace(ctx, &mesav1.DeleteWorkspaceRequest{NameOrId: "proj"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestShellBridgeEchoes(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()
	_, _ = c.CreateWorkspace(ctx, &mesav1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})

	stream, err := c.Shell(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&mesav1.ShellClientMsg{Msg: &mesav1.ShellClientMsg_Open{
		Open: &mesav1.OpenShell{NameOrId: "proj", Cols: 80, Rows: 24},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&mesav1.ShellClientMsg{Msg: &mesav1.ShellClientMsg_Data{Data: []byte("ping")}}); err != nil {
		t.Fatal(err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if string(msg.GetData()) != "ping" {
		t.Fatalf("echo mismatch: %q", msg.GetData())
	}
}
