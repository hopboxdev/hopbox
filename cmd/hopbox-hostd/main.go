//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"

	"google.golang.org/grpc"

	"github.com/hopboxdev/silo"

	pb "github.com/hopboxdev/hopbox/gen/hostd/v1"
	"github.com/hopboxdev/hopbox/internal/hostd"
)

func main() {
	var (
		listenAddr = flag.String("listen", "127.0.0.1:9090", "gRPC listen address")
		zfsPool    = flag.String("zfs-pool", "silo", "ZFS pool name")
		agentBin   = flag.String("agent-binary", "", "path to hop-agent linux binary (required)")
		hostIP     = flag.String("host-ip", "", "public IP of this host (required)")
		portMin    = flag.Int("port-min", 51820, "minimum UDP port for WireGuard")
		portMax    = flag.Int("port-max", 52820, "maximum UDP port for WireGuard")
		dataDir    = flag.String("data-dir", "/var/lib/hopbox-hostd", "data directory for state files")
	)
	flag.Parse()

	if *agentBin == "" || *hostIP == "" {
		flag.Usage()
		os.Exit(1)
	}

	if _, err := os.Stat(*agentBin); err != nil {
		log.Fatalf("hop-agent binary not found: %s", *agentBin)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, *listenAddr, *zfsPool, *agentBin, *hostIP, *portMin, *portMax, *dataDir); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, listenAddr, zfsPool, agentBin, hostIP string, portMin, portMax int, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	log.Println("Initializing Silo runtime...")
	rt, err := silo.Init(ctx, silo.Options{
		ZFSPool:   zfsPool,
		CIDRRange: "172.16.0.0/16",
	})
	if err != nil {
		return fmt.Errorf("silo init: %w", err)
	}

	ports, err := hostd.NewPortAllocator(portMin, portMax,
		filepath.Join(dataDir, "ports.json"))
	if err != nil {
		return fmt.Errorf("port allocator: %w", err)
	}

	prov := hostd.NewProvisioner(agentBin, hostIP)

	srv := hostd.NewServer(hostd.ServerConfig{
		Runtime:       rt,
		Provisioner:   prov,
		PortAllocator: ports,
		HostIP:        hostIP,
		Defaults: hostd.WorkspaceDefaults{
			Image:    "ubuntu-dev",
			VCPUs:    2,
			MemoryMB: 2048,
			DiskGB:   10,
		},
	})

	grpcServer := grpc.NewServer()
	pb.RegisterHostServiceServer(grpcServer, srv)

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	log.Printf("hopbox-hostd listening on %s", listenAddr)

	// Graceful shutdown on context cancel.
	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")
		grpcServer.GracefulStop()
	}()

	if err := grpcServer.Serve(ln); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
