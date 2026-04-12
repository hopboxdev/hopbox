package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type TransferCmd struct {
	File string `arg:"" help:"Local file to upload. Append :/remote/path to set destination."`
	Box  string `help:"Box to upload to (overrides default)." short:"b"`
}

func parseTransferTarget(input string) (local, remote string) {
	i := strings.LastIndex(input, ":")
	if i < 0 {
		return input, "~/"
	}
	local = input[:i]
	remote = input[i+1:]
	if remote == "" {
		remote = "~/"
	}
	return local, remote
}

func (c *TransferCmd) Run() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	local, remote := parseTransferTarget(c.File)

	if _, err := os.Stat(local); err != nil {
		return fmt.Errorf("file not found: %s", local)
	}

	sshUser := cfg.sshUserWithBox(c.Box)
	dest := fmt.Sprintf("%s@%s:%s", sshUser, cfg.Server, remote)

	args := []string{
		"-O",
		"-P", strconv.Itoa(cfg.Port),
		local,
		dest,
	}

	fmt.Printf("Uploading %s -> %s:%s\n", local, cfg.sshUserWithBox(c.Box), remote)

	cmd := exec.Command("scp", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
