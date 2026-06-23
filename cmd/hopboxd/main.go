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
	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/api"
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/plugin"
	"github.com/hopboxdev/hopbox/internal/sshca"
	"github.com/hopboxdev/hopbox/providers/identity/static"
	"github.com/hopboxdev/hopbox/providers/ingress/subdomain"
)

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

	// agent hub: resolve tokens via the store, report connect state to the store.
	hub := agenthub.New().
		WithResolver(func(ctx context.Context, token string) (string, error) {
			w, err := st.GetByToken(ctx, token)
			if err != nil {
				return "", err
			}
			return w.ID, nil
		}).
		WithSink(storeSink{store: st, tenant: cfg.Tenant})

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

	// One subdomain ingress provider instance is shared by the reconciler (which
	// Exposes endpoints into its route table) and the gateway (which Lookups them).
	ingress := subdomain.New(cfg.GatewayZone)

	// SSH user CA: auto-created on first run. Every workspace trusts its public
	// key, so `hopbox login` certs work without distributing per-box keys.
	caSigner, err := sshca.LoadOrCreateCA(cfg.SSHCAPath)
	if err != nil {
		return fmt.Errorf("ssh ca: %w", err)
	}
	caTrustLine := string(ssh.MarshalAuthorizedKey(caSigner.PublicKey()))
	log.Printf("hopboxd: ssh user CA %s (workspaces trust %s)", cfg.SSHCAPath, strings.TrimSpace(caTrustLine))

	authKeys := loadAuthorizedKeys(cfg.AuthorizedKeysFile)
	rec := reconciler.New(st, compute, storage, ingress, reconciler.Config{
		AgentAddr: cfg.AgentAdvertise,
		Agent: ports.AgentImage{
			ImageRef:       cfg.AgentImageRef,
			BinaryPath:     cfg.AgentBinaryPath,
			TargetPath:     cfg.AgentTargetPath,
			HostBinaryPath: cfg.AgentBin, // M1 dev fast-path
		},
		TrustedUserCA:  caTrustLine,
		AuthorizedKeys: authKeys,
	})
	go rec.Run(ctx)

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
	// Multi-user auth: if a users file is configured, authenticate every call to a
	// Principal; otherwise run open (single-user) with the default owner.
	var opts []grpc.ServerOption
	if users := loadUsers(cfg.UsersFile, cfg.Tenant); len(users) > 0 {
		idp := static.New(users)
		opts = append(opts,
			grpc.UnaryInterceptor(api.AuthUnaryInterceptor(idp)),
			grpc.StreamInterceptor(api.AuthStreamInterceptor(idp)),
		)
		log.Printf("hopboxd: multi-user auth on (%d principals from %s)", len(users), cfg.UsersFile)
	}
	gs := grpc.NewServer(opts...)
	hopboxv1.RegisterWorkspaceServiceServer(gs, api.NewServer(st, hub, cfg.Tenant, cfg.Owner, caSigner))
	go func() { <-ctx.Done(); gs.GracefulStop() }()

	log.Printf("hopboxd: API on %s", cfg.APIAddr)
	return gs.Serve(apiLn)
}
