package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/configserver"
)

type ConfigServerCmd struct {
	Port int `help:"Port to bind on 127.0.0.1." required:""`
}

func doConfigServer(port int) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config-server: home dir: %v\n", err)
		os.Exit(1)
	}

	devcontainerPath := filepath.Join(home, ".devcontainer", "devcontainer.json")

	if err := os.MkdirAll(filepath.Dir(devcontainerPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "config-server: mkdir .devcontainer: %v\n", err)
		os.Exit(1)
	}

	srv := &configserver.Server{
		Port:             port,
		DevcontainerPath: devcontainerPath,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := srv.ListenAndServe(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "config-server: %v\n", err)
		os.Exit(1)
	}
}
