package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type TransferCmd struct {
	Source string `arg:"" help:"Source path. Prefix with : for remote (e.g. :~/file.txt)."`
	Dest   string `arg:"" optional:"" help:"Destination path. Prefix with : for remote. Defaults to ~/ (upload) or ./ (download)."`
	Box    string `help:"Box to use (overrides default)." short:"b"`
}

func (c *TransferCmd) Run() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	sshUser := cfg.sshUserWithBox(c.Box)
	portArgs := []string{"-O", "-P", strconv.Itoa(cfg.Port)}

	if strings.HasPrefix(c.Source, ":") {
		// Download: remote -> local
		remotePath := c.Source[1:]
		if remotePath == "" {
			remotePath = "~/"
		}
		localPath := c.Dest
		if localPath == "" {
			localPath = "."
		}

		remote := fmt.Sprintf("%s@%s:%s", sshUser, cfg.Server, remotePath)
		args := append(portArgs, remote, localPath)

		fmt.Printf("Downloading %s:%s -> %s\n", sshUser, remotePath, localPath)

		cmd := exec.Command("scp", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Upload: local -> remote
	local := c.Source
	if _, err := os.Stat(local); err != nil {
		return fmt.Errorf("file not found: %s", local)
	}

	remotePath := "~/"
	if c.Dest != "" {
		if strings.HasPrefix(c.Dest, ":") {
			remotePath = c.Dest[1:]
		} else {
			remotePath = c.Dest
		}
	}
	if remotePath == "" {
		remotePath = "~/"
	}

	remote := fmt.Sprintf("%s@%s:%s", sshUser, cfg.Server, remotePath)
	args := append(portArgs, local, remote)

	fmt.Printf("Uploading %s -> %s:%s\n", local, sshUser, remotePath)

	cmd := exec.Command("scp", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
