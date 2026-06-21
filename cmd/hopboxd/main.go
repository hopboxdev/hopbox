// Command hopboxd is the Hopbox control plane: store + reconciler + agent hub + gRPC API.
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/api"
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/plugin"
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

	rec := reconciler.New(st, compute, storage, ingress, reconciler.Config{
		AgentAddr: cfg.AgentAdvertise,
		Agent: ports.AgentImage{
			ImageRef:       cfg.AgentImageRef,
			BinaryPath:     cfg.AgentBinaryPath,
			TargetPath:     cfg.AgentTargetPath,
			HostBinaryPath: cfg.AgentBin, // M1 dev fast-path
		},
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
	gs := grpc.NewServer()
	hopboxv1.RegisterWorkspaceServiceServer(gs, api.NewServer(st, hub, cfg.Tenant, cfg.Owner))
	go func() { <-ctx.Done(); gs.GracefulStop() }()

	log.Printf("hopboxd: API on %s", cfg.APIAddr)
	return gs.Serve(apiLn)
}
