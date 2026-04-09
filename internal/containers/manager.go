package containers

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func ContainerName(username, boxname string) string {
	return fmt.Sprintf("hopbox-%s-%s", username, boxname)
}

type Manager struct {
	cli *client.Client
}

func NewManager(cli *client.Client) *Manager {
	return &Manager{cli: cli}
}

func (m *Manager) EnsureRunning(ctx context.Context, username, boxname, imageTag, homePath string) (string, error) {
	name := ContainerName(username, boxname)

	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	if len(containers) > 0 {
		c := containers[0]
		if c.State != "running" {
			if err := m.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
				return "", fmt.Errorf("start container: %w", err)
			}
		}
		return c.ID, nil
	}

	cfg := &container.Config{
		Image:      imageTag,
		User:       "dev",
		WorkingDir: "/home/dev",
		Cmd:        []string{"sleep", "infinity"},
	}
	hostCfg := &container.HostConfig{
		Binds: []string{fmt.Sprintf("%s:/home/dev", homePath)},
	}

	log.Printf("[container] creating %s with bind mount %s -> /home/dev", name, homePath)

	resp, err := m.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := m.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}

func (m *Manager) Exec(ctx context.Context, containerID string, cmd []string, env []string, stdin io.Reader, stdout io.Writer, resizeCh <-chan [2]uint) error {
	execCfg := container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}

	execResp, err := m.cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return fmt.Errorf("exec create: %w", err)
	}

	attachResp, err := m.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return fmt.Errorf("exec attach: %w", err)
	}
	defer attachResp.Close()

	go func() {
		for size := range resizeCh {
			_ = m.cli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
				Height: uint(size[1]),
				Width:  uint(size[0]),
			})
		}
	}()

	// stdin -> container (background; will unblock when we close attachResp)
	go func() {
		io.Copy(attachResp.Conn, stdin)
	}()

	// container -> stdout (blocks until process exits)
	io.Copy(stdout, attachResp.Reader)

	// Process exited — close connection to unblock stdin goroutine
	attachResp.Close()

	return nil
}

func (m *Manager) ContainerIP(ctx context.Context, containerID string) (string, error) {
	info, err := m.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}

	// Try top-level IP first, then check per-network settings
	// (containerd snapshotter only populates the per-network map)
	ip := info.NetworkSettings.IPAddress
	if ip == "" {
		for _, net := range info.NetworkSettings.Networks {
			if net.IPAddress != "" {
				ip = net.IPAddress
				break
			}
		}
	}
	if ip == "" {
		return "", fmt.Errorf("container %s has no IP address", containerID)
	}
	return ip, nil
}
