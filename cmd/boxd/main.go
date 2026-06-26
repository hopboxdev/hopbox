// Command boxd is the standalone compute-box daemon: `ssh box@host` spawns an
// ephemeral box and bridges the session in. It is the box product with NO
// dev-env compiled in — it wires box.Engine + the box reconciler + the agent hub
// + the SSH front door over an in-memory box store and a compute provider.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os/signal"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/sshca"
	"github.com/hopboxdev/hopbox/internal/sshfront"
)

func main() {
	sshAddr := flag.String("ssh-addr", ":2222", "front-door SSH listen (username=box spec, key=identity)")
	agentListen := flag.String("agent-listen", ":7777", "agent reverse-dial listen address")
	advertise := flag.String("advertise", "host.docker.internal:7777", "address the in-box agent dials back")
	agentBin := flag.String("agent-bin", "", "host path of the linux hopbox-agent binary to side-load")
	hostKeyPath := flag.String("host-key", "./boxd-ssh-host-key", "front-door SSH host key (auto-created)")
	image := flag.String("default-image", "alpine", "image when the spec names none")
	cpus := flag.Float64("default-cpus", 2, "CPU cap (vCPU) per box; 0 = unlimited")
	memMB := flag.Int64("default-mem-mb", 2048, "memory cap (MB) per box; 0 = unlimited")
	flag.Parse()

	if err := run(*sshAddr, *agentListen, *advertise, *agentBin, *hostKeyPath, *image, *cpus, *memMB); err != nil {
		log.Fatal(err)
	}
}

func run(sshAddr, agentListen, advertise, agentBin, hostKeyPath, image string, cpus float64, memMB int64) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store := newMemStore()

	compute, err := newCompute(advertise) // build-tagged: docker (or a stub without -tags docker)
	if err != nil {
		return err
	}

	// agent hub: resolve the agent's bootstrap token to its box, and report
	// connect/disconnect back to the store + wake the reconciler.
	rec := box.NewReconciler(store, compute, box.ReconcileConfig{
		AgentAddr: advertise,
		Agent:     ports.AgentImage{HostBinaryPath: agentBin, TargetPath: "/hopbox/hopbox-agent"},
	})
	hub := agenthub.New().
		WithResolver(func(_ context.Context, token string) (string, error) {
			b, err := store.GetByToken(token)
			if err != nil {
				return "", err
			}
			return b.ID, nil
		}).
		WithSink(boxSink{store: store, rec: rec})

	go rec.Run(ctx)

	agentLn, err := net.Listen("tcp", agentListen)
	if err != nil {
		return err
	}
	go func() {
		log.Printf("boxd: agent gateway on %s (advertise %s)", agentListen, advertise)
		if err := hub.Serve(ctx, agentLn); err != nil {
			log.Printf("boxd: agent hub stopped: %v", err)
		}
	}()

	engine := box.NewEngine(store, rec.Trigger, box.EngineConfig{
		Tenant:        "default",
		DefaultImage:  image,
		Backends:      []string{"docker"},
		DefaultFlavor: box.Flavor{MemMB: memMB, CPUMillis: int64(cpus * 1000)},
	})

	hostKey, err := sshca.LoadOrCreateCA(hostKeyPath)
	if err != nil {
		return err
	}
	front := sshfront.NewServer(engine, hub, hostKey, nil) // AnyKey: the client key is the identity
	frontLn, err := net.Listen("tcp", sshAddr)
	if err != nil {
		return err
	}
	log.Printf("boxd: SSH front door on %s (default image %s)", sshAddr, image)
	return front.Serve(ctx, frontLn)
}

// boxSink records agent connect state on the box and wakes the reconciler.
type boxSink struct {
	store *memStore
	rec   *box.Reconciler
}

func (s boxSink) SetAgentConnected(ctx context.Context, id string, connected bool) {
	b, err := s.store.Get(ctx, "", id)
	if err != nil {
		return
	}
	b.AgentConnected = connected
	if err := s.store.Update(ctx, b); err != nil {
		return
	}
	s.rec.Trigger(id, b.TenantID)
}
