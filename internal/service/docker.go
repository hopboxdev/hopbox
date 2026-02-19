package service

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DockerBackend manages services via the docker CLI.
type DockerBackend struct {
	// Image is the Docker image to run.
	Image string
	// Cmd overrides the container's default command.
	Cmd []string
	// Env are extra environment variables.
	Env map[string]string
	// Ports maps host ports to container ports (e.g. "8080:80").
	Ports []string
	// Volumes maps host paths to container paths (e.g. "/data:/var/lib/data").
	Volumes []string
}

// Start launches the container (docker run --rm -d).
func (d *DockerBackend) Start(_ context.Context, name string) error {
	args := []string{"run", "--rm", "-d", "--name", name}
	for _, p := range d.Ports {
		args = append(args, "-p", p)
	}
	for _, v := range d.Volumes {
		args = append(args, "-v", v)
	}
	for k, v := range d.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, d.Image)
	args = append(args, d.Cmd...)

	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %q: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Stop kills and removes the container.
func (d *DockerBackend) Stop(name string) error {
	out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm %q: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsRunning checks whether the named container is running.
func (d *DockerBackend) IsRunning(name string) (bool, error) {
	var out bytes.Buffer
	cmd := exec.Command("docker", "ps", "--filter", "name="+name, "--format", "{{.Names}}")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("docker ps: %w", err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(line) == name {
			return true, nil
		}
	}
	return false, nil
}
