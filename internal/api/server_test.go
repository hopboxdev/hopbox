package api_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
	"github.com/hopboxdev/hopbox/internal/agentproto"
	"github.com/hopboxdev/hopbox/internal/api"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/providers/identity/static"
)

// testToken sends an api token like the real CLI does.
type testToken struct{ tok string }

func (t testToken) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + t.tok}, nil
}
func (testToken) RequireTransportSecurity() bool { return false }

func TestMultiUserOwnerIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir() + "/mu.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, caPriv, _ := ed25519.GenerateKey(rand.Reader)
	ca, _ := ssh.NewSignerFromSigner(caPriv)
	srv := api.NewServer(s, &fakeHub{connected: true}, "default", "system", ca)

	idp := static.New(map[string]ports.Principal{
		"tok-alice": {ID: "alice", TenantID: "default", Roles: []string{"owner"}},
		"tok-bob":   {ID: "bob", TenantID: "default", Roles: []string{"owner"}},
	})
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer(
		grpc.UnaryInterceptor(api.AuthUnaryInterceptor(idp)),
		grpc.StreamInterceptor(api.AuthStreamInterceptor(idp)),
	)
	hopboxv1.RegisterWorkspaceServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	client := func(creds ...grpc.DialOption) hopboxv1.WorkspaceServiceClient {
		opts := append([]grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}, creds...)
		conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return hopboxv1.NewWorkspaceServiceClient(conn)
	}
	alice := client(grpc.WithPerRPCCredentials(testToken{"tok-alice"}))
	bob := client(grpc.WithPerRPCCredentials(testToken{"tok-bob"}))
	anon := client()

	if _, err := alice.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "abox", ImageRef: "ubuntu:24.04"}); err != nil {
		t.Fatalf("alice create: %v", err)
	}
	if _, err := bob.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "bbox", ImageRef: "ubuntu:24.04"}); err != nil {
		t.Fatalf("bob create: %v", err)
	}

	// each user lists only their own box, owned by them.
	la, _ := alice.ListWorkspaces(ctx, &hopboxv1.ListWorkspacesRequest{})
	if len(la.Workspaces) != 1 || la.Workspaces[0].Name != "abox" || la.Workspaces[0].Owner != "alice" {
		t.Fatalf("alice list = %+v", la.Workspaces)
	}
	lb, _ := bob.ListWorkspaces(ctx, &hopboxv1.ListWorkspacesRequest{})
	if len(lb.Workspaces) != 1 || lb.Workspaces[0].Name != "bbox" {
		t.Fatalf("bob list = %+v", lb.Workspaces)
	}

	// bob cannot see or touch alice's box.
	if _, err := bob.GetWorkspace(ctx, &hopboxv1.GetWorkspaceRequest{NameOrId: "abox"}); status.Code(err) != codes.NotFound {
		t.Fatalf("bob get abox: want NotFound, got %v", err)
	}

	// certs are scoped to the caller.
	_, userPriv, _ := ed25519.GenerateKey(rand.Reader)
	us, _ := ssh.NewSignerFromSigner(userPriv)
	pub := string(ssh.MarshalAuthorizedKey(us.PublicKey()))
	ra, _ := alice.IssueSSHCert(ctx, &hopboxv1.IssueSSHCertRequest{PublicKey: pub})
	rb, _ := bob.IssueSSHCert(ctx, &hopboxv1.IssueSSHCertRequest{PublicKey: pub})
	if ra.Principal != "alice" || rb.Principal != "bob" {
		t.Fatalf("cert principals: alice=%q bob=%q", ra.Principal, rb.Principal)
	}

	// no token -> unauthenticated.
	if _, err := anon.ListWorkspaces(ctx, &hopboxv1.ListWorkspacesRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anon list: want Unauthenticated, got %v", err)
	}
}

func TestIssueSSHCert(t *testing.T) {
	_, caPriv, _ := ed25519.GenerateKey(rand.Reader)
	ca, _ := ssh.NewSignerFromSigner(caPriv)
	srv := api.NewServer(nil, nil, "default", "alice", ca)

	_, userPriv, _ := ed25519.GenerateKey(rand.Reader)
	userSigner, _ := ssh.NewSignerFromSigner(userPriv)
	pubLine := ssh.MarshalAuthorizedKey(userSigner.PublicKey())

	resp, err := srv.IssueSSHCert(context.Background(), &hopboxv1.IssueSSHCertRequest{PublicKey: string(pubLine)})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if resp.Principal != "alice" {
		t.Fatalf("principal = %q, want alice", resp.Principal)
	}
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(resp.Certificate))
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	cert, ok := pk.(*ssh.Certificate)
	if !ok {
		t.Fatal("response is not a certificate")
	}
	checker := &ssh.CertChecker{IsUserAuthority: func(k ssh.PublicKey) bool {
		return string(k.Marshal()) == string(ca.PublicKey().Marshal())
	}}
	if err := checker.CheckCert("alice", cert); err != nil {
		t.Fatalf("issued cert fails verification: %v", err)
	}

	// no CA configured -> issuance disabled.
	if _, err := api.NewServer(nil, nil, "default", "alice", nil).
		IssueSSHCert(context.Background(), &hopboxv1.IssueSSHCertRequest{PublicKey: string(pubLine)}); err == nil {
		t.Fatal("expected error when CA is not configured")
	}
}

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

func (f *fakeHub) OpenSSH(string) (io.ReadWriteCloser, error) {
	c1, c2 := net.Pipe()
	go func() { _, _ = io.Copy(c2, c2) }() // echo, like OpenShell
	return c1, nil
}

func (f *fakeHub) OpenExec(_ string, cmd []string) (io.ReadWriteCloser, error) {
	c1, c2 := net.Pipe()
	// far ("agent") end: emit "ran:<cmd>", echo any stdin back as stdout, exit 0.
	go func() {
		defer c2.Close()
		_ = agentproto.WriteExecData(c2, agentproto.ExecStdout, []byte("ran:"+cmd[0]))
		for {
			typ, data, _, err := agentproto.ReadExecFrame(c2)
			if err != nil {
				break
			}
			if typ == agentproto.ExecStdin {
				_ = agentproto.WriteExecData(c2, agentproto.ExecStdout, data)
			}
			if typ == agentproto.ExecStdinClose {
				break
			}
		}
		_ = agentproto.WriteExecExit(c2, 0)
	}()
	return c1, nil
}

func dialer(t *testing.T) (hopboxv1.WorkspaceServiceClient, func()) {
	t.Helper()
	s, err := sqlite.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(s, &fakeHub{connected: true}, "default", "alice", nil)

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	hopboxv1.RegisterWorkspaceServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return hopboxv1.NewWorkspaceServiceClient(conn), func() {
		_ = conn.Close()
		gs.Stop()
		_ = s.Close()
	}
}

func TestCreateGetListDelete(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()

	w, err := c.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Phase != "Pending" || w.Name != "proj" {
		t.Fatalf("bad create: %+v", w)
	}
	got, err := c.GetWorkspace(ctx, &hopboxv1.GetWorkspaceRequest{NameOrId: "proj"})
	if err != nil || got.Id != w.Id {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	list, err := c.ListWorkspaces(ctx, &hopboxv1.ListWorkspacesRequest{})
	if err != nil || len(list.Workspaces) != 1 {
		t.Fatalf("list: %d err=%v", len(list.Workspaces), err)
	}
	if _, err := c.DeleteWorkspace(ctx, &hopboxv1.DeleteWorkspaceRequest{NameOrId: "proj"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestGetWorkspaceNotFound(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()
	_, err := c.GetWorkspace(ctx, &hopboxv1.GetWorkspaceRequest{NameOrId: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v (code=%s)", err, status.Code(err))
	}
}

func TestShellBridgeEchoes(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()
	_, _ = c.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})

	stream, err := c.Shell(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&hopboxv1.ShellClientMsg{Msg: &hopboxv1.ShellClientMsg_Open{
		Open: &hopboxv1.OpenShell{NameOrId: "proj", Cols: 80, Rows: 24},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&hopboxv1.ShellClientMsg{Msg: &hopboxv1.ShellClientMsg_Data{Data: []byte("ping")}}); err != nil {
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

// drainExec reads an exec stream to completion, returning combined stdout and
// the exit code.
func drainExec(t *testing.T, stream hopboxv1.WorkspaceService_ExecClient) (string, int32) {
	t.Helper()
	var out string
	var code int32 = -1
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if d := msg.GetStdout(); d != nil {
			out += string(d)
		}
		if _, ok := msg.Msg.(*hopboxv1.ExecServerMsg_ExitCode); ok {
			code = msg.GetExitCode()
		}
	}
	return out, code
}

func TestExecStreamsOutputAndExit(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()
	_, _ = c.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})

	stream, err := c.Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&hopboxv1.ExecClientMsg{Msg: &hopboxv1.ExecClientMsg_Open{
		Open: &hopboxv1.ExecOpen{NameOrId: "proj", Cmd: []string{"ls", "-la"}},
	}}); err != nil {
		t.Fatal(err)
	}
	_ = stream.CloseSend()
	if out, code := drainExec(t, stream); out != "ran:ls" || code != 0 {
		t.Fatalf("exec output=%q code=%d", out, code)
	}
}

func TestExecForwardsStdin(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()
	_, _ = c.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})

	stream, err := c.Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&hopboxv1.ExecClientMsg{Msg: &hopboxv1.ExecClientMsg_Open{
		Open: &hopboxv1.ExecOpen{NameOrId: "proj", Cmd: []string{"cat"}},
	}})
	_ = stream.Send(&hopboxv1.ExecClientMsg{Msg: &hopboxv1.ExecClientMsg_Stdin{Stdin: []byte("piped-in")}})
	_ = stream.CloseSend()
	out, code := drainExec(t, stream)
	if !strings.Contains(out, "piped-in") || code != 0 {
		t.Fatalf("stdin not echoed: out=%q code=%d", out, code)
	}
}

func TestExecRequiresCmd(t *testing.T) {
	ctx := context.Background()
	c, done := dialer(t)
	defer done()
	_, _ = c.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "proj", ImageRef: "ubuntu:24.04"})
	stream, err := c.Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&hopboxv1.ExecClientMsg{Msg: &hopboxv1.ExecClientMsg_Open{
		Open: &hopboxv1.ExecOpen{NameOrId: "proj"},
	}})
	_ = stream.CloseSend()
	if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}
