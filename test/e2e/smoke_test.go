//go:build docker

package e2e

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/api"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
)

func TestEndToEndShell(t *testing.T) {
	agentBin := os.Getenv("HOPBOX_TEST_AGENT_BIN")
	if agentBin == "" {
		t.Skip("set HOPBOX_TEST_AGENT_BIN to the linux/amd64 hopbox-agent binary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	st, err := sqlite.Open(dir + "/e2e.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Address the in-container agent dials back to reach this in-process hopboxd.
	// Defaults to the Docker host gateway (host.docker.internal); override via
	// HOPBOX_TEST_ADVERTISE when hopboxd runs somewhere the container reaches by a
	// different address (e.g. on an sdev devbox: the devbox's own bridge IP).
	advertise := "host.docker.internal:7799"
	if a := os.Getenv("HOPBOX_TEST_ADVERTISE"); a != "" {
		advertise = a
	}
	compute, err := dockerprov.New(advertise)
	if err != nil {
		t.Fatal(err)
	}
	storage := localfs.New(dir + "/homes")

	hub := agenthub.New().
		WithResolver(func(ctx context.Context, token string) (string, error) {
			w, err := st.GetByToken(ctx, token)
			if err != nil {
				return "", err
			}
			return w.ID, nil
		}).
		WithSink(sink{st})

	agentLn, err := net.Listen("tcp", ":7799")
	if err != nil {
		t.Fatal(err)
	}
	go hub.Serve(ctx, agentLn)

	rec := reconciler.New(st, compute, storage, nil, reconciler.Config{
		AgentAddr: advertise,
		Agent:     ports.AgentImage{HostBinaryPath: agentBin, TargetPath: "/hopbox/hopbox-agent"},
		Interval:  500 * time.Millisecond,
	})
	go rec.Run(ctx)

	apiLn, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	hopboxv1.RegisterWorkspaceServiceServer(gs, api.NewServer(st, hub, "default", "dev"))
	go gs.Serve(apiLn)
	defer gs.Stop()

	conn, err := grpc.NewClient(apiLn.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := hopboxv1.NewWorkspaceServiceClient(conn)

	w, err := c.CreateWorkspace(ctx, &hopboxv1.CreateWorkspaceRequest{Name: "e2e", ImageRef: "ubuntu:24.04"})
	if err != nil {
		t.Fatal(err)
	}
	// Teardown: DeleteWorkspace only flags the workspace Destroying for the
	// reconciler, but this test cancels the reconciler's ctx on return before it
	// can converge — which would leak the container. So destroy the instance
	// synchronously here via the compute provider, using the InstanceRef the
	// store recorded. (Belt-and-suspenders: also issue the API delete.)
	defer func() {
		_, _ = c.DeleteWorkspace(context.Background(), &hopboxv1.DeleteWorkspaceRequest{NameOrId: w.Id})
		if got, gerr := st.GetWorkspace(context.Background(), "default", w.Id); gerr == nil && got.InstanceRef != "" {
			_ = compute.Destroy(context.Background(), got.InstanceRef)
		}
	}()

	// wait until the agent connects and phase becomes Running
	deadline := time.Now().Add(60 * time.Second)
	for {
		got, _ := c.GetWorkspace(ctx, &hopboxv1.GetWorkspaceRequest{NameOrId: "e2e"})
		if got != nil && got.Phase == "Running" && got.AgentConnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace never reached Running (last: %+v)", got)
		}
		time.Sleep(time.Second)
	}

	stream, err := c.Shell(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&hopboxv1.ShellClientMsg{Msg: &hopboxv1.ShellClientMsg_Open{
		Open: &hopboxv1.OpenShell{NameOrId: "e2e", Cols: 80, Rows: 24, Cmd: "/bin/sh -c 'echo HOPBOX_E2E_OK; exit'"},
	}})

	var out strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF || err != nil {
			break
		}
		out.Write(msg.GetData())
	}
	if !strings.Contains(out.String(), "HOPBOX_E2E_OK") {
		t.Fatalf("missing marker in shell output: %q", out.String())
	}
}

type sink struct{ st *sqlite.Store }

func (s sink) SetAgentConnected(ctx context.Context, id string, connected bool) {
	w, err := s.st.GetWorkspace(ctx, "default", id)
	if err != nil {
		return
	}
	w.AgentConnected = connected
	_ = s.st.UpdateWorkspace(ctx, w)
}
