package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

type ExposeCmd struct {
	Port int    `arg:"" help:"Port to forward."`
	Box  string `help:"Box to tunnel to (overrides default)." short:"b"`
}

func (c *ExposeCmd) Run() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	sshUser := cfg.sshUserWithBox(c.Box)
	portStr := strconv.Itoa(c.Port)
	forward := fmt.Sprintf("%s:localhost:%s", portStr, portStr)

	args := []string{
		"-p", strconv.Itoa(cfg.Port),
		"-L", forward,
		"-N",
		sshUser + "@" + cfg.Server,
	}

	fmt.Printf("Forwarding localhost:%d -> box:%d (ctrl-c to stop)\n", c.Port, c.Port)

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	return cmd.Run()
}
