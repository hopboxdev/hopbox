package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"

	"github.com/hopboxdev/hopbox/internal/admin"
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/control"
	"github.com/hopboxdev/hopbox/internal/gateway"
	"github.com/hopboxdev/hopbox/internal/metrics"
	"github.com/hopboxdev/hopbox/internal/users"
)

func main() {
	configPath := flag.String("config", "", "path to config.toml (default: ./config.toml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	initLogger(cfg)

	slog.Info("config loaded", "config", cfg)

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

	// Root context for long-lived background goroutines (collector, refresher).
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// Ensure base image is built
	templatesDir := findTemplatesDir()
	imageTag, err := containers.EnsureBaseImage(ctx, cli, templatesDir)
	if err != nil {
		log.Fatalf("ensure base image: %v", err)
	}
	slog.Info("base image ready", "image", imageTag)

	// Initialize user store
	store := users.NewStore(usersDir)

	// Start metrics collector and totals refresher.
	collector := metrics.NewCollector(cli)
	go collector.Start(rootCtx)
	startTotalsRefresher(rootCtx, store)

	// Initialize container manager
	mgr := containers.NewManager(cli, cfg)

	// Initialize link store and wire it into the manager
	linkStore := control.NewLinkStore()
	mgr.SetLinkStore(linkStore)

	// Start admin web UI if enabled
	if cfg.Admin.Enabled {
		if cfg.Admin.Password == "" {
			log.Fatal("admin.password must be set when admin is enabled")
		}
		adminSrv := admin.NewAdminServer(&cfg, store, mgr, cli)
		go func() {
			slog.Info("admin UI listening", "port", cfg.Admin.Port, "user", cfg.Admin.Username)
			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("admin server error", "err", err)
			}
		}()
	}

	// Start SSH server
	srv, err := gateway.NewServer(cfg, store, mgr, cli, imageTag, linkStore)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down")
		rootCancel()
		mgr.Shutdown()
		srv.Close()
		slog.Info("shutdown complete")
	}()

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
	}
}

func startTotalsRefresher(ctx context.Context, store *users.Store) {
	refresh := func() {
		// Walk the users dir directly and skip symlinks so alternative-key
		// links (see Store.LinkKey) don't double-count the same user/boxes.
		entries, err := os.ReadDir(store.Dir())
		if err != nil {
			return
		}
		userCount := 0
		boxes := 0
		for _, e := range entries {
			info, err := os.Lstat(filepath.Join(store.Dir(), e.Name()))
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				continue
			}
			userDir := filepath.Join(store.Dir(), e.Name())
			if _, err := os.Stat(filepath.Join(userDir, "user.toml")); err != nil {
				continue
			}
			userCount++
			names, _ := containers.ListBoxes(userDir)
			boxes += len(names)
		}
		metrics.UsersTotal.Set(float64(userCount))
		metrics.BoxesTotal.Set(float64(boxes))
	}
	refresh()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh()
			}
		}
	}()
}

func initLogger(cfg config.Config) {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
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
