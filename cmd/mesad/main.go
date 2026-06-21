// Command mesad is the Mesa control plane: store + reconciler + agent hub + gRPC API.
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

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
	"github.com/mesadev/mesa/internal/agenthub"
	"github.com/mesadev/mesa/internal/api"
	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/core/reconciler"
	"github.com/mesadev/mesa/internal/core/store/sqlite"
	"github.com/mesadev/mesa/internal/plugin"
	"github.com/mesadev/mesa/providers/ingress/subdomain"
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
		log.Printf("mesad: agent gateway on %s (advertise %s)", cfg.AgentListen, cfg.AgentAdvertise)
		if err := hub.Serve(ctx, agentLn); err != nil {
			log.Printf("mesad: agent hub stopped: %v", err)
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

	// Service gateway (mesa-gw): resolves Host -> workspace via the ingress route
	// table and proxies INTO the workspace over the agent reverse-connection.
	if cfg.GatewayAddr != "" {
		gwLn, err := net.Listen("tcp", cfg.GatewayAddr)
		if err != nil {
			return err
		}
		gwSrv := &http.Server{Handler: newGateway(ingress, hub)}
		go func() { <-ctx.Done(); _ = gwSrv.Close() }()
		go func() {
			log.Printf("mesad: gateway on %s (zone %s)", cfg.GatewayAddr, cfg.GatewayZone)
			if err := gwSrv.Serve(gwLn); err != nil && err != http.ErrServerClosed {
				log.Printf("mesad: gateway stopped: %v", err)
			}
		}()
	}

	apiLn, err := net.Listen("tcp", cfg.APIAddr)
	if err != nil {
		return err
	}
	gs := grpc.NewServer()
	mesav1.RegisterWorkspaceServiceServer(gs, api.NewServer(st, hub, cfg.Tenant, cfg.Owner))
	go func() { <-ctx.Done(); gs.GracefulStop() }()

	log.Printf("mesad: API on %s", cfg.APIAddr)
	return gs.Serve(apiLn)
}
