//go:build docker

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/events"
	"github.com/hopboxdev/hopbox/internal/sshfront"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
	"os"
)

// TestEndToEndSSHFrontDoor exercises the krillbox-style front door for real: a
// stock golang.org/x/crypto/ssh client dials the front-door listener with the
// username as a workspace spec and its key as the identity. The front door
// spawns an ephemeral docker box, the agent dials back, the front door bridges
// an exec into it, and on client disconnect the reconciler reaps the box.
//
// This is the end-to-end path the PR flagged as build/vet-only: ssh.NewServerConn
// handshake -> serveSession -> waitReady -> Attach -> bridge -> reap.
func TestEndToEndSSHFrontDoor(t *testing.T) {
	agentBin := os.Getenv("HOPBOX_TEST_AGENT_BIN")
	if agentBin == "" {
		t.Skip("set HOPBOX_TEST_AGENT_BIN to the linux hopbox-agent binary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	st, err := sqlite.Open(dir + "/front.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	advertise := "host.docker.internal:7798"
	if a := os.Getenv("HOPBOX_TEST_ADVERTISE"); a != "" {
		advertise = a
	}
	compute, err := dockerprov.New(advertise)
	if err != nil {
		t.Fatal(err)
	}
	storage := localfs.New(dir + "/homes")

	bus := events.NewInProc()
	defer bus.Close()

	hub := agenthub.New().
		WithResolver(func(ctx context.Context, token string) (string, error) {
			w, err := st.GetByToken(ctx, token)
			if err != nil {
				return "", err
			}
			return w.ID, nil
		}).
		WithSink(triggerSink{st: st, bus: bus})

	agentLn, err := net.Listen("tcp", ":7798")
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
	if err := bus.Subscribe(rec.Trigger); err != nil {
		t.Fatal(err)
	}

	// Front door: username=spec, key=identity (AnyKey). DefaultImage carries the
	// colon (ubuntu:24.04) the username grammar can't, so the client username is
	// just the bare workspace name.
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromSigner(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	mgr := sshfront.New(st, bus.Publish, sshfront.Config{
		Tenant:       "default",
		DefaultImage: "ubuntu:24.04",
		Backends:     []string{"docker"},
	})
	front := sshfront.NewServer(mgr, hub, hostKey, nil)
	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go front.Serve(ctx, frontLn)

	// Belt-and-suspenders teardown: if the reap path leaks, destroy the box.
	defer func() {
		if got, gerr := st.GetByName(context.Background(), "default", "frontbox"); gerr == nil && got.InstanceRef != "" {
			_ = compute.Destroy(context.Background(), got.InstanceRef)
		}
	}()

	// Stock SSH client. The key is the identity; the username is the spec.
	_, userPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	userSigner, err := ssh.NewSignerFromSigner(userPriv)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ssh.Dial("tcp", frontLn.Addr().String(), &ssh.ClientConfig{
		User:            "frontbox", // spec: workspace "frontbox", default image, grace 0
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(userSigner)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	})
	if err != nil {
		t.Fatalf("front-door ssh dial: %v", err)
	}

	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	// One-shot `ssh frontbox@host echo ...`: the client half-closes stdin at once,
	// so this is the non-interactive path that exposed the bridge teardown race.
	// Output blocks until the box is spawned, the agent dialled back (waitReady),
	// and the bridged command ran — and now drains fully before teardown.
	out, err := sess.Output("echo HOPBOX_FRONTDOOR_OK")
	if err != nil {
		t.Fatalf("front-door exec: %v", err)
	}
	if !strings.Contains(string(out), "HOPBOX_FRONTDOOR_OK") {
		t.Fatalf("missing marker in front-door output: %q", out)
	}
	_ = sess.Close()
	_ = client.Close()

	// Disconnect -> releaser clears Attached -> trigger -> reconciler reaps the
	// grace-0 ephemeral box (Running -> Destroying -> destroyed -> gone from store).
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, gerr := st.GetByName(context.Background(), "default", "frontbox")
		if errors.Is(gerr, store.ErrNotFound) {
			break // reaped
		}
		if time.Now().After(deadline) {
			got, _ := st.GetByName(context.Background(), "default", "frontbox")
			t.Fatalf("ephemeral box not reaped after disconnect (last: %+v)", got)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// triggerSink mirrors hopboxd's storeSink: record agent connect state, then wake
// the reconciler over the events bus (the hybrid loop's event half).
type triggerSink struct {
	st  *sqlite.Store
	bus events.Bus
}

func (s triggerSink) SetAgentConnected(ctx context.Context, id string, connected bool) {
	w, err := s.st.GetWorkspace(ctx, "default", id)
	if err != nil {
		return
	}
	w.AgentConnected = connected
	if err := s.st.UpdateWorkspace(ctx, w); err != nil {
		return
	}
	s.bus.Publish(id, "default")
}
