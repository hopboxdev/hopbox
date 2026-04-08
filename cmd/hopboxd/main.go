package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/docker/docker/client"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/gateway"
	"github.com/hopboxdev/hopbox/internal/users"
)

func main() {
	configPath := flag.String("config", "", "path to config.toml (default: ./config.toml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Resolve data dir to absolute path (Docker bind mounts require absolute paths)
	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}

	// Ensure data directory exists
	usersDir := filepath.Join(cfg.DataDir, "users")
	if err := os.MkdirAll(usersDir, 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	// Verify Docker is reachable
	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
		log.Fatalf("cannot reach Docker daemon: %v", err)
	}

	// Ensure base image is built
	templatesDir := findTemplatesDir()
	imageTag, err := containers.EnsureBaseImage(ctx, cli, templatesDir)
	if err != nil {
		log.Fatalf("ensure base image: %v", err)
	}
	log.Printf("using base image: %s", imageTag)

	// Initialize user store
	store := users.NewStore(usersDir)

	// Initialize container manager
	mgr := containers.NewManager(cli)

	// Start SSH server
	srv, err := gateway.NewServer(cfg, store, mgr, imageTag)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		srv.Close()
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Printf("server stopped: %v", err)
	}
}

func findTemplatesDir() string {
	if info, err := os.Stat("templates"); err == nil && info.IsDir() {
		return "templates"
	}

	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "templates")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	log.Fatal("templates directory not found")
	return ""
}
