// Command mesad is the Mesa control plane: store + reconciler + agent hub + gRPC API.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
	"github.com/mesadev/mesa/internal/agenthub"
	"github.com/mesadev/mesa/internal/api"
	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/reconciler"
	"github.com/mesadev/mesa/internal/core/store"
	"github.com/mesadev/mesa/internal/core/store/sqlite"
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

	compute, err := newCompute(cfg.AgentAdvertise)
	if err != nil {
		return err
	}
	storage := newStorage(cfg) // defined in storage_localfs.go below

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

	rec := reconciler.New(st, compute, storage, reconciler.Config{
		AgentAddr: cfg.AgentAdvertise,
		AgentPath: cfg.AgentBin,
	})
	go rec.Run(ctx)

	apiLn, err := net.Listen("tcp", cfg.APIAddr)
	if err != nil {
		return err
	}
	gs := grpc.NewServer()
	mesav1.RegisterWorkspaceServiceServer(gs, api.NewServer(st, hub, cfg.Tenant, cfg.Owner))
	go func() { <-ctx.Done(); gs.GracefulStop() }()

	log.Printf("mesad: API on %s", cfg.APIAddr)
	_ = store.ErrNotFound // keep store import explicit for readers
	return gs.Serve(apiLn)
}
