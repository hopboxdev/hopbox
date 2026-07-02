// Command hopboxd is the standalone compute-box daemon: `ssh box@host` spawns an
// ephemeral box and bridges the session in. It is the box product with NO
// dev-env compiled in — it wires box.Engine + the box reconciler + the agent hub
// + the SSH front door over a persistent (sqlite) box store and a compute provider.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/boxmeta"
	"github.com/hopboxdev/hopbox/internal/core/boxsqlite"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/mcp"
	"github.com/hopboxdev/hopbox/internal/sshca"
	"github.com/hopboxdev/hopbox/internal/sshfront"
)

func main() {
	sshAddr := flag.String("ssh-addr", ":2222", "front-door SSH listen (username=box spec, key=identity)")
	agentListen := flag.String("agent-listen", ":7777", "agent reverse-dial listen address")
	advertise := flag.String("advertise", "", "address the in-box agent dials back (default: derived from the --compute gateway + agent port)")
	agentBin := flag.String("agent-bin", "", "host path of the linux hopbox-agent binary to side-load (docker)")
	hostKeyPath := flag.String("host-key", "./hopboxd-ssh-host-key", "front-door SSH host key (auto-created)")
	image := flag.String("default-image", "alpine", "image when the spec names none (docker)")
	cpus := flag.Float64("default-cpus", 2, "CPU cap (vCPU) per box; 0 = unlimited")
	memMB := flag.Int64("default-mem-mb", 2048, "memory cap (MB) per box; 0 = unlimited")
	db := flag.String("db", "./hopboxd.db", "box database path")
	metaAddr := flag.String("meta-addr", ":8090", "metadata API listen address (boxes reach it by source IP)")
	guestBin := flag.String("guest-bin", "", "host path of the linux box-guest binary to side-load into boxes (docker)")
	compute := flag.String("compute", "docker", "compute backend: docker | microvm")
	fcBin := flag.String("fc-bin", "/usr/local/bin/firecracker", "firecracker binary (microvm)")
	fcKernel := flag.String("fc-kernel", "/opt/hopbox-microvm/vmlinux", "vmlinux kernel (microvm)")
	fcImagesDir := flag.String("fc-images-dir", "/opt/hopbox-microvm/images", "base-image catalog dir; image <name> -> <dir>/<name>.ext4 (microvm)")
	fcRunDir := flag.String("fc-rundir", "/var/lib/hopbox/microvm", "per-VM working dir (microvm)")
	fcBridge := flag.String("fc-bridge", "", "microvm host bridge (default hopbox-vmnet); set with --fc-subnet to run a second fleet beside another daemon")
	fcSubnet := flag.String("fc-subnet", "", "microvm /24 base, first three octets (default 10.0.0; gateway is .1)")
	autoSuspend := flag.Bool("auto-suspend", false, "persistent boxes that auto-suspend when idle, waking on reconnect (vs ephemeral reap) — the account/workspace tier")
	idleTimeout := flag.Duration("idle-timeout", 5*time.Minute, "suspend a box after this long idle (with --auto-suspend)")
	grace := flag.Duration("grace", 2*time.Minute, "ephemeral reconnect window: keep a box this long after disconnect before reaping (0 = reap immediately)")
	mcpAddr := flag.String("mcp-addr", "", "serve the AI-control MCP plane here (unix:/path or host:port); empty = off")
	surfaceAddr := flag.String("surface-addr", "", "serve AI-rendered canvas surfaces over HTTP here (host:port); empty = off")
	surfaceURL := flag.String("surface-url", "", "public base URL for surface links (default: http://<surface-addr>)")
	flag.Parse()

	if err := run(cfg{
		sshAddr: *sshAddr, agentListen: *agentListen, advertise: *advertise, agentBin: *agentBin,
		hostKeyPath: *hostKeyPath, image: *image, cpus: *cpus, memMB: *memMB, db: *db,
		metaAddr: *metaAddr, guestBin: *guestBin, compute: *compute,
		fcBin: *fcBin, fcKernel: *fcKernel, fcImagesDir: *fcImagesDir, fcRunDir: *fcRunDir,
		fcBridge: *fcBridge, fcSubnet: *fcSubnet,
		autoSuspend: *autoSuspend, idleTimeout: *idleTimeout, grace: *grace,
		mcpAddr: *mcpAddr, surfaceAddr: *surfaceAddr, surfaceURL: *surfaceURL,
	}); err != nil {
		log.Fatal(err)
	}
}

type cfg struct {
	sshAddr, agentListen, advertise, agentBin, hostKeyPath, image, db, metaAddr, guestBin string
	compute, fcBin, fcKernel, fcImagesDir, fcRunDir, fcBridge, fcSubnet, mcpAddr          string
	surfaceAddr, surfaceURL                                                               string
	cpus                                                                                  float64
	memMB                                                                                 int64
	autoSuspend                                                                           bool
	idleTimeout, grace                                                                    time.Duration
}

// gatewayHost is the address the in-box agent + box-guest reach the host at, per
// backend: docker boxes use the magic host alias; microVMs use the bridge gateway
// (.1 of the fleet's /24).
func gatewayHost(c cfg) string {
	if c.compute == "microvm" {
		sub := c.fcSubnet
		if sub == "" {
			sub = "10.0.0"
		}
		return sub + ".1"
	}
	return "host.docker.internal"
}

// portOf extracts the port from a listen address (":7777" -> "7777"), falling
// back to def.
func portOf(addr, def string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
		return p
	}
	return def
}

const engineTenant = "default" // hopboxd is single-tenant

// resetSessions clears the Attached flag on every box at startup: no SSH session
// survives a restart, so a box marked attached is stale. Ephemeral boxes then
// count down their grace and reap; persistent boxes stay suspended until a real
// reconnect (rather than spuriously resuming).
func resetSessions(ctx context.Context, store box.Store) {
	boxes, err := store.List(ctx, engineTenant)
	if err != nil {
		return
	}
	for _, b := range boxes {
		if b.Attached {
			b.Attached = false
			_ = store.Update(ctx, b)
		}
	}
}

func run(c cfg) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := boxsqlite.Open(c.db)
	if err != nil {
		return err
	}
	defer store.Close()
	resetSessions(ctx, store) // no SSH session survives a restart; clear stale Attached

	// The agent + box-guest reach the host at the backend's gateway. The agent
	// dials `advertise`; box-guest reads $BOX_META; both derived from one host.
	gwHost := gatewayHost(c)
	agentPort := portOf(c.agentListen, "7777")
	metaPort := portOf(c.metaAddr, "8090")
	advertise := c.advertise
	if advertise == "" {
		advertise = net.JoinHostPort(gwHost, agentPort)
	}
	metaURL := "http://" + net.JoinHostPort(gwHost, metaPort)

	// One write seam backs every metadata mutation (heartbeat + owner commands):
	// resolve the calling box by IP, apply, persist.
	mutate := func(ctx context.Context, ip string, fn func(*box.Box)) error {
		b, err := store.GetByIP(ctx, ip)
		if err != nil {
			return err
		}
		fn(b)
		return store.Update(ctx, b)
	}
	go func() {
		mux := boxmeta.New(store.GetByIP, mutate, box.IdleConfig{Timeout: c.idleTimeout, LoadThreshold: box.DefaultIdle.LoadThreshold}).Handler()
		mln, err := net.Listen("tcp", c.metaAddr)
		if err != nil {
			log.Printf("hopboxd: metadata API listen %s: %v", c.metaAddr, err)
			return
		}
		log.Printf("hopboxd: metadata API on %s (boxes reach %s)", c.metaAddr, metaURL)
		_ = (&http.Server{Handler: mux}).Serve(mln)
	}()

	compute, err := newCompute(c, advertise, metaPort) // build-tagged backend (docker/microvm); stub otherwise
	if err != nil {
		return err
	}
	if cl, ok := compute.(io.Closer); ok {
		defer cl.Close() // release shared backend resources (e.g. the microVM origin loop)
	}

	// agent hub: resolve the agent's bootstrap token to its box, and report
	// connect/disconnect back to the store + wake the reconciler.
	rec := box.NewReconciler(store, compute, box.ReconcileConfig{
		AgentAddr:  advertise,
		Agent:      ports.AgentImage{HostBinaryPath: c.agentBin, TargetPath: "/hopbox/hopbox-agent"},
		MetaURL:    metaURL,
		GuestBin:   c.guestBin,
		Idle:       box.IdleConfig{Timeout: c.idleTimeout, LoadThreshold: box.DefaultIdle.LoadThreshold},
		TrustedSSH: true, // the front door authenticates the user, then proxies the SSH session into the box
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
		log.Printf("hopboxd: agent gateway on %s (advertise %s)", c.agentListen, advertise)
		if err := hub.Serve(ctx, agentLn); err != nil {
			log.Printf("hopboxd: agent hub stopped: %v", err)
		}
	}()

	engine := box.NewEngine(store, rec.Trigger, box.EngineConfig{
		Tenant:        engineTenant,
		DefaultImage:  c.image,
		Backends:      []string{"docker"},
		DefaultFlavor: box.Flavor{MemMB: c.memMB, CPUMillis: int64(c.cpus * 1000)},
		Persistent:    func(string) bool { return c.autoSuspend }, // hopboxd: one global tier
		DefaultGrace:  c.grace,
	})

	hostKey, err := sshca.LoadOrCreateCA(c.hostKeyPath)
	if err != nil {
		return err
	}
	front := sshfront.NewServer(engine, hub, hostKey, nil) // AnyKey: the client key is the identity
	if il, ok := compute.(ports.ImageLister); ok {
		front = front.WithImages(il.Images) // advertise the catalog in the connect banner
	}
	frontLn, err := net.Listen("tcp", c.sshAddr)
	if err != nil {
		return err
	}
	log.Printf("hopboxd: SSH front door on %s (default image %s)", c.sshAddr, c.image)

	// AI-control plane: serve the MCP protocol (fleet resource + box tools + pushed
	// changes) over the real engine + hub, so an AI drives the live fleet.
	if c.mcpAddr != "" {
		surfaceURL := c.surfaceURL
		if surfaceURL == "" && c.surfaceAddr != "" {
			surfaceURL = "http://" + c.surfaceAddr
		}
		be := mcp.NewEngineBackend(engine, hub, surfaceURL)

		// canvas loop: serve AI-rendered surfaces (+ capture interactions) over HTTP.
		if c.surfaceAddr != "" {
			sln, err := net.Listen("tcp", c.surfaceAddr)
			if err != nil {
				return err
			}
			go func() {
				log.Printf("hopboxd: canvas surfaces on %s (base %s)", c.surfaceAddr, surfaceURL)
				_ = (&http.Server{Handler: be.Surfaces().Handler()}).Serve(sln)
			}()
		}

		network, a := "tcp", c.mcpAddr
		if s, ok := strings.CutPrefix(c.mcpAddr, "unix:"); ok {
			network, a = "unix", s
			_ = os.Remove(a)
		}
		mcpLn, err := net.Listen(network, a)
		if err != nil {
			return err
		}
		go func() {
			log.Printf("hopboxd: AI-control MCP plane on %s", c.mcpAddr)
			if err := mcp.Listen(ctx, mcpLn, be); err != nil {
				log.Printf("hopboxd: mcp plane stopped: %v", err)
			}
		}()
	}

	err = front.Serve(ctx, frontLn)

	// Graceful shutdown: snapshot persistent boxes so the next start resumes them
	// (disk + memory) rather than re-provisioning. Fresh context — ctx is cancelled.
	drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	log.Printf("hopboxd: draining — suspending persistent boxes")
	rec.Drain(drainCtx, engineTenant)
	cancel()
	return err
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
