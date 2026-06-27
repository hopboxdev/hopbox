// Command boxd is the standalone compute-box daemon: `ssh box@host` spawns an
// ephemeral box and bridges the session in. It is the box product with NO
// dev-env compiled in — it wires box.Engine + the box reconciler + the agent hub
// + the SSH front door over a persistent (sqlite) box store and a compute provider.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/boxmeta"
	"github.com/hopboxdev/hopbox/internal/core/boxsqlite"
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
	db := flag.String("db", "./boxd.db", "box database path")
	metaAddr := flag.String("meta-addr", ":8090", "metadata API listen address (boxes reach it by source IP)")
	guestBin := flag.String("guest-bin", "", "host path of the linux box-guest binary to side-load into boxes")
	flag.Parse()

	if err := run(cfg{
		sshAddr: *sshAddr, agentListen: *agentListen, advertise: *advertise, agentBin: *agentBin,
		hostKeyPath: *hostKeyPath, image: *image, cpus: *cpus, memMB: *memMB, db: *db,
		metaAddr: *metaAddr, guestBin: *guestBin,
	}); err != nil {
		log.Fatal(err)
	}
}

type cfg struct {
	sshAddr, agentListen, advertise, agentBin, hostKeyPath, image, db, metaAddr, guestBin string
	cpus                                                                                  float64
	memMB                                                                                 int64
}

func run(c cfg) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := boxsqlite.Open(c.db)
	if err != nil {
		return err
	}
	defer store.Close()

	// Metadata API: a box reaches it by source IP (no credential in the box). The
	// box-guest client in each box reads $BOX_META = http://host.docker.internal:PORT.
	metaPort := c.metaAddr
	if _, p, err := net.SplitHostPort(c.metaAddr); err == nil && p != "" {
		metaPort = p
	}
	go func() {
		mux := boxmeta.New(store.GetByIP).Handler()
		mln, err := net.Listen("tcp", c.metaAddr)
		if err != nil {
			log.Printf("boxd: metadata API listen %s: %v", c.metaAddr, err)
			return
		}
		log.Printf("boxd: metadata API on %s", c.metaAddr)
		_ = (&http.Server{Handler: mux}).Serve(mln)
	}()

	compute, err := newCompute(c.advertise, metaPort) // build-tagged: docker (or a stub without -tags docker)
	if err != nil {
		return err
	}

	// agent hub: resolve the agent's bootstrap token to its box, and report
	// connect/disconnect back to the store + wake the reconciler.
	rec := box.NewReconciler(store, compute, box.ReconcileConfig{
		AgentAddr: c.advertise,
		Agent:     ports.AgentImage{HostBinaryPath: c.agentBin, TargetPath: "/hopbox/hopbox-agent"},
		MetaURL:   "http://host.docker.internal:" + metaPort,
		GuestBin:  c.guestBin,
	})
	hub := agenthub.New().
		WithResolver(func(ctx context.Context, token string) (string, error) {
			b, err := store.GetByToken(ctx, token)
			if err != nil {
				return "", err
			}
			return b.ID, nil
		}).
		WithSink(boxSink{store: store, rec: rec})

	go rec.Run(ctx)

	agentLn, err := net.Listen("tcp", c.agentListen)
	if err != nil {
		return err
	}
	go func() {
		log.Printf("boxd: agent gateway on %s (advertise %s)", c.agentListen, c.advertise)
		if err := hub.Serve(ctx, agentLn); err != nil {
			log.Printf("boxd: agent hub stopped: %v", err)
		}
	}()

	engine := box.NewEngine(store, rec.Trigger, box.EngineConfig{
		Tenant:        "default",
		DefaultImage:  c.image,
		Backends:      []string{"docker"},
		DefaultFlavor: box.Flavor{MemMB: c.memMB, CPUMillis: int64(c.cpus * 1000)},
	})

	hostKey, err := sshca.LoadOrCreateCA(c.hostKeyPath)
	if err != nil {
		return err
	}
	front := sshfront.NewServer(engine, hub, hostKey, nil) // AnyKey: the client key is the identity
	frontLn, err := net.Listen("tcp", c.sshAddr)
	if err != nil {
		return err
	}
	log.Printf("boxd: SSH front door on %s (default image %s)", c.sshAddr, c.image)
	return front.Serve(ctx, frontLn)
}

// boxSink records agent connect state on the box and wakes the reconciler.
type boxSink struct {
	store box.Store
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
