// Command hopboxd is the Hopbox control plane: store + reconciler + agent hub + gRPC API.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
	"github.com/hopboxdev/hopbox/internal/account"
	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/api"
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/boxmeta"
	"github.com/hopboxdev/hopbox/internal/core/boxstore"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/plugin"
	"github.com/hopboxdev/hopbox/internal/sshca"
	"github.com/hopboxdev/hopbox/internal/sshfront"
	"github.com/hopboxdev/hopbox/providers/identity/oidc"
	"github.com/hopboxdev/hopbox/providers/identity/static"
	"github.com/hopboxdev/hopbox/providers/ingress/subdomain"
)

// portOf extracts the port from a listen address (":8090" -> "8090"), falling back to def.
func portOf(addr, def string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
		return p
	}
	return def
}

// splitComma parses a comma-separated flag value, trimming blanks.
func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// loadAuthorizedKeys returns the SSH authorized_keys injected into every
// workspace: the contents of file if set, else the HOPBOX_AUTHORIZED_KEYS env.
// Empty disables SSH access.
func loadAuthorizedKeys(file string) string {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			log.Printf("hopboxd: authorized-keys %s: %v (ssh disabled)", file, err)
			return ""
		}
		log.Printf("hopboxd: ssh enabled (authorized keys from %s)", file)
		return string(b)
	}
	if env := os.Getenv("HOPBOX_AUTHORIZED_KEYS"); env != "" {
		log.Printf("hopboxd: ssh enabled (authorized keys from HOPBOX_AUTHORIZED_KEYS)")
		return env
	}
	return ""
}

// loadUsers parses a token->principal file (lines `<token> <principal>`, '#'
// comments) into the static identity provider's key map. Empty path => open
// single-user mode (no entries).
func loadUsers(file, tenant string) map[string]ports.Principal {
	if file == "" {
		return nil
	}
	f, err := os.Open(file)
	if err != nil {
		log.Printf("hopboxd: users %s: %v (auth disabled)", file, err)
		return nil
	}
	defer f.Close()
	users := map[string]ports.Principal{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		users[parts[0]] = ports.Principal{ID: parts[1], TenantID: tenant, Roles: []string{"owner"}}
	}
	return users
}

func run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	inprocCompute, err := newCompute(cfg) // nil-returning stub without -tags docker/k8s
	if err != nil && cfg.ComputeTransport != "remote" {
		return err
	}
	compute, err := plugin.LoadCompute(plugin.ProviderConfig{
		Kind: cfg.ComputeKind, Transport: cfg.ComputeTransport, RemoteAddr: cfg.ComputeRemote,
	}, inprocCompute)
	if err != nil {
		return err
	}

	storage, err := plugin.LoadStorage(plugin.ProviderConfig{
		Kind: cfg.StorageKind, Transport: cfg.StorageTransport, RemoteAddr: cfg.StorageRemote,
	}, newStorage(cfg))
	if err != nil {
		return err
	}

	// One subdomain ingress provider instance is shared by the reconciler (which
	// Exposes endpoints into its route table) and the gateway (which Lookups them).
	ingress := subdomain.New(cfg.GatewayZone)

	// SSH user CA: either trust an external CA's public key (the org issues certs
	// with their own tooling — Vault/Smallstep/Teleport) or run a built-in CA,
	// auto-created on first run, that `hopbox login` issues short-lived certs from.
	var caSigner ssh.Signer // nil when trusting an external CA (built-in issuance off)
	var caTrustLine string
	if cfg.SSHCAPubFile != "" {
		b, err := os.ReadFile(cfg.SSHCAPubFile)
		if err != nil {
			return fmt.Errorf("ssh-ca-pub: %w", err)
		}
		caTrustLine = strings.TrimSpace(string(b))
		log.Printf("hopboxd: trusting external SSH CA from %s (built-in `hopbox login` issuance disabled)", cfg.SSHCAPubFile)
	} else {
		signer, err := sshca.LoadOrCreateCA(cfg.SSHCAPath)
		if err != nil {
			return fmt.Errorf("ssh ca: %w", err)
		}
		caSigner = signer
		caTrustLine = string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
		log.Printf("hopboxd: ssh user CA %s (workspaces trust %s)", cfg.SSHCAPath, strings.TrimSpace(caTrustLine))
	}

	// Account tier: keys in the registered-keys file are accounts (persistent,
	// auto-suspending boxes); unknown keys are anonymous (ephemeral). The same
	// directory authenticates the front door and decides the engine's tier.
	var frontAuthority sshfront.Authority // nil => AnyKey (everyone anonymous)
	var persistentTier func(string) bool
	if cfg.AccountsFile != "" {
		dir, err := account.Load(cfg.AccountsFile)
		if err != nil {
			return fmt.Errorf("accounts: %w", err)
		}
		frontAuthority, persistentTier = dir, dir.IsAccount
		log.Printf("hopboxd: account tier on (%d registered keys from %s)", dir.Len(), cfg.AccountsFile)
	}

	authKeys := loadAuthorizedKeys(cfg.AuthorizedKeysFile)

	// Box metadata API (opt-in via --meta-addr): a box reaches it by source IP to
	// learn about itself + tune its lifecycle (box-guest). The box reaches it at
	// the same host it reaches the agent hub (the advertise host), so derive
	// $BOX_META from there. Works for any backend (docker bridge, microVM gateway).
	var metaURL string
	if cfg.MetaAddr != "" {
		metaHost := cfg.AgentAdvertise
		if h, _, err := net.SplitHostPort(cfg.AgentAdvertise); err == nil {
			metaHost = h
		}
		metaURL = "http://" + net.JoinHostPort(metaHost, portOf(cfg.MetaAddr, "8090"))
		resolve := func(ctx context.Context, ip string) (*box.Box, error) {
			w, err := st.GetByIP(ctx, ip)
			if err != nil {
				return nil, err
			}
			return &w.Box, nil
		}
		mutate := func(ctx context.Context, ip string, fn func(*box.Box)) error {
			w, err := st.GetByIP(ctx, ip)
			if err != nil {
				return err
			}
			fn(&w.Box)
			return st.UpdateWorkspace(ctx, w)
		}
		mln, err := net.Listen("tcp", cfg.MetaAddr)
		if err != nil {
			return fmt.Errorf("metadata listen %s: %w", cfg.MetaAddr, err)
		}
		go func() {
			log.Printf("hopboxd: box metadata API on %s (boxes reach %s)", cfg.MetaAddr, metaURL)
			_ = (&http.Server{Handler: boxmeta.New(resolve, mutate, box.DefaultIdle).Handler()}).Serve(mln)
		}()
	}

	// One reconciler: box.Reconciler (lifecycle + suspend/persistence) with the
	// dev-env's storage-home + ingress folded in as hooks. The box-view of the
	// workspace store is boxstore.New(st).
	hooks := boxstore.NewHooks(st, storage, ingress, boxstore.HooksConfig{
		TrustedUserCA:  caTrustLine,
		AuthorizedKeys: authKeys,
	})
	rec := box.NewReconciler(boxstore.New(st), compute, box.ReconcileConfig{
		AgentAddr: cfg.AgentAdvertise,
		Agent: ports.AgentImage{
			ImageRef:       cfg.AgentImageRef,
			BinaryPath:     cfg.AgentBinaryPath,
			TargetPath:     cfg.AgentTargetPath,
			HostBinaryPath: cfg.AgentBin, // M1 dev fast-path
		},
		MetaURL:  metaURL,      // $BOX_META in each box (box-guest); "" when --meta-addr off
		GuestBin: cfg.GuestBin, // side-load box-guest into docker boxes; microVM bakes it in
		Hooks:    hooks,
	})
	go rec.Run(ctx)

	// reconcile wake-up bus: in-proc by default (a direct call to rec.Trigger);
	// NATS fans wake-ups across nodes. Either way the reconciler's interval sweep
	// remains the backstop, so a lost wake-up only delays — never drops — a reap.
	bus, err := newEventBus(cfg)
	if err != nil {
		return err
	}
	defer bus.Close()
	if err := bus.Subscribe(rec.Trigger); err != nil {
		return fmt.Errorf("events subscribe: %w", err)
	}

	// agent hub: resolve tokens via the store, report connect state to the store,
	// and wake the reconciler on every connect/disconnect (hybrid event path).
	hub := agenthub.New().
		WithResolver(func(ctx context.Context, token string) (string, error) {
			w, err := st.GetByToken(ctx, token)
			if err != nil {
				return "", err
			}
			return w.ID, nil
		}).
		WithSink(storeSink{store: st, tenant: cfg.Tenant, trigger: bus.Publish})

	agentLn, err := net.Listen("tcp", cfg.AgentListen)
	if err != nil {
		return err
	}
	go func() {
		log.Printf("hopboxd: agent gateway on %s (advertise %s)", cfg.AgentListen, cfg.AgentAdvertise)
		if err := hub.Serve(ctx, agentLn); err != nil {
			log.Printf("hopboxd: agent hub stopped: %v", err)
		}
	}()

	// krillbox-style SSH front door: `ssh proj:python+5m@host` — the username is a
	// workspace spec, the client key is the identity. Spawns an ephemeral box and
	// bridges the session into it; on disconnect the reconciler reaps it.
	if cfg.SSHAddr != "" {
		hostKey, err := sshca.LoadOrCreateCA(cfg.SSHHostKeyPath)
		if err != nil {
			return fmt.Errorf("ssh front-door host key: %w", err)
		}
		engine := box.NewEngine(boxstore.New(st), bus.Publish, box.EngineConfig{
			Tenant:       cfg.Tenant,
			DefaultImage: cfg.SSHDefaultImage,
			Backends:     []string{cfg.ComputeKind},
			DefaultFlavor: box.Flavor{
				MemMB:     cfg.SSHDefaultMemMB,
				CPUMillis: int64(cfg.SSHDefaultCPUs * 1000),
			},
			Persistent: persistentTier, // accounts -> persistent; anonymous -> ephemeral
		})
		front := sshfront.NewServer(engine, hub, hostKey, frontAuthority) // nil => AnyKey
		frontLn, err := net.Listen("tcp", cfg.SSHAddr)
		if err != nil {
			return err
		}
		go func() {
			log.Printf("hopboxd: SSH front door on %s (default image %s)", cfg.SSHAddr, cfg.SSHDefaultImage)
			if err := front.Serve(ctx, frontLn); err != nil {
				log.Printf("hopboxd: SSH front door stopped: %v", err)
			}
		}()
	}

	// Service gateway (hopbox-gw): resolves Host -> workspace via the ingress route
	// table and proxies INTO the workspace over the agent reverse-connection.
	if cfg.GatewayAddr != "" {
		gwLn, err := net.Listen("tcp", cfg.GatewayAddr)
		if err != nil {
			return err
		}
		gwSrv := &http.Server{Handler: newGateway(ingress, hub)}
		go func() { <-ctx.Done(); _ = gwSrv.Close() }()
		go func() {
			log.Printf("hopboxd: gateway on %s (zone %s)", cfg.GatewayAddr, cfg.GatewayZone)
			if err := gwSrv.Serve(gwLn); err != nil && err != http.ErrServerClosed {
				log.Printf("hopboxd: gateway stopped: %v", err)
			}
		}()
	}

	// Gateway tunnel: lets a standalone hopbox-gw fleet reach these workspaces
	// (resolves Host + bridges to the agent forward stream, server-side).
	if cfg.TunnelAddr != "" {
		tunLn, err := net.Listen("tcp", cfg.TunnelAddr)
		if err != nil {
			return err
		}
		tunSrv := newTunnelServer(ingress, hub)
		go func() {
			log.Printf("hopboxd: gateway tunnel on %s", cfg.TunnelAddr)
			if err := tunSrv.Serve(ctx, tunLn); err != nil {
				log.Printf("hopboxd: gateway tunnel stopped: %v", err)
			}
		}()
	}

	apiLn, err := net.Listen("tcp", cfg.APIAddr)
	if err != nil {
		return err
	}
	// Multi-user auth. OIDC (SSO) takes precedence; else a static token file; else
	// open single-user mode (no interceptor, default owner).
	var idp ports.Identity
	switch {
	case cfg.OIDCIssuer != "":
		ver, err := oidc.NewVerifier(ctx, cfg.OIDCIssuer, cfg.OIDCAudience)
		if err != nil {
			return err
		}
		idp = oidc.New(ver, oidc.Config{
			TenantID:       cfg.Tenant,
			PrincipalClaim: cfg.OIDCPrincipalClaim,
			AdminGroups:    splitComma(cfg.OIDCAdminGroups),
		})
		log.Printf("hopboxd: OIDC auth on (issuer %s)", cfg.OIDCIssuer)
	default:
		if users := loadUsers(cfg.UsersFile, cfg.Tenant); len(users) > 0 {
			idp = static.New(users)
			log.Printf("hopboxd: multi-user auth on (%d principals from %s)", len(users), cfg.UsersFile)
		}
	}
	var opts []grpc.ServerOption
	if idp != nil {
		opts = append(opts,
			grpc.UnaryInterceptor(api.AuthUnaryInterceptor(idp)),
			grpc.StreamInterceptor(api.AuthStreamInterceptor(idp)),
		)
	}
	gs := grpc.NewServer(opts...)
	hopboxv1.RegisterWorkspaceServiceServer(gs, api.NewServer(st, hub, cfg.Tenant, cfg.Owner, caSigner))
	go func() { <-ctx.Done(); gs.GracefulStop() }()

	log.Printf("hopboxd: API on %s", cfg.APIAddr)
	return gs.Serve(apiLn)
}
