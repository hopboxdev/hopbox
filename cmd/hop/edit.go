package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"time"
	"strings"
	"syscall"
)

type EditCmd struct {
	Box  string `help:"Box to edit (overrides default)." short:"b"`
	JSON bool   `help:"Open raw devcontainer.json in $EDITOR instead of browser." name:"json"`
}

func (c *EditCmd) Run() error {
	if c.JSON {
		return c.runJSONMode()
	}
	return c.runBrowserMode()
}

func (c *EditCmd) runBrowserMode() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	localPort, err := freePort()
	if err != nil {
		return fmt.Errorf("pick local port: %w", err)
	}

	remotePort := 49152 + rand.Intn(16383)

	sshUser := cfg.sshUserWithBox(c.Box)
	forward := fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort)

	args := []string{
		"-p", strconv.Itoa(cfg.Port),
		"-L", forward,
		"-o", "ExitOnForwardFailure=yes",
		sshUser + "@" + cfg.Server,
		"hop", "config-server", "--port", strconv.Itoa(remotePort),
	}

	fmt.Printf("Starting config server for box %q…\n", resolveBox(cfg, c.Box))

	cmd := exec.Command("ssh", args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "LISTENING :") {
				close(ready)
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-done:
		return fmt.Errorf("config-server exited before becoming ready")
	}

	url := fmt.Sprintf("http://localhost:%d", localPort)
	fmt.Printf("Config server ready: %s\n", url)
	fmt.Println("Close browser tab or press Ctrl-C to stop.")
	openBrowser(url)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
		time.Sleep(3 * time.Second)
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGKILL)
		}
	}()

	<-done
	fmt.Println("Config server stopped.")
	return nil
}

func (c *EditCmd) runJSONMode() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	sshUser := cfg.sshUserWithBox(c.Box)
	sshArgs := []string{"-p", strconv.Itoa(cfg.Port), sshUser + "@" + cfg.Server}

	downloadArgs := append(sshArgs, "cat", "/home/dev/.devcontainer/devcontainer.json")
	out, err := exec.Command("ssh", downloadArgs...).Output()
	if err != nil {
		return fmt.Errorf("download devcontainer.json: %w", err)
	}

	tmp, err := os.CreateTemp("", "hopbox-devcontainer-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	tmp.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		fmt.Println("Editor exited non-zero — discarding changes.")
		return nil
	}

	updated, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read updated file: %w", err)
	}

	var check interface{}
	if err := json.Unmarshal(updated, &check); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid JSON — discarding changes: %v\n", err)
		fmt.Print("Re-open editor? [y/N]: ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(strings.TrimSpace(ans)) == "y" {
			return c.runJSONMode()
		}
		return nil
	}

	uploadArgs := append(sshArgs, "tee", "/home/dev/.devcontainer/devcontainer.json")
	uploadCmd := exec.Command("ssh", uploadArgs...)
	uploadCmd.Stdin = strings.NewReader(string(updated))
	uploadCmd.Stderr = os.Stderr
	if err := uploadCmd.Run(); err != nil {
		return fmt.Errorf("upload devcontainer.json: %w", err)
	}

	fmt.Println("✓ devcontainer.json updated — rebuild fires on next SSH connect.")
	return nil
}

// freePort finds a free TCP port on 127.0.0.1 by binding and releasing.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// waitProcess returns a channel that closes when cmd exits.
func waitProcess(cmd *exec.Cmd) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		cmd.Wait()
		close(ch)
	}()
	return ch
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = exec.Command("open", url).Start()
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	default:
		fmt.Printf("Open in browser: %s\n", url)
		return
	}
	if err != nil {
		fmt.Printf("Open in browser: %s\n", url)
	}
}

func resolveBox(cfg *hopConfig, override string) string {
	if override != "" {
		return override
	}
	if cfg.DefaultBox != "" {
		return cfg.DefaultBox
	}
	return "default"
}
