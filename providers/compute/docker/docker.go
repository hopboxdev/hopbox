//go:build docker

// Package docker is the M1 Compute provider. It maps a neutral ProvisionRequest
// onto a Docker container that side-loads the mesa-agent binary (bind-mounted,
// read-only) and runs it as the entrypoint. No Docker type crosses ports.*.
package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"

	"github.com/mesadev/mesa/internal/core/ports"
)

const (
	labelWorkspace = "mesa.workspace_id"
	agentTarget    = "/mesa/mesa-agent"
)

type Provider struct {
	cli *client.Client
}

var _ ports.Compute = (*Provider)(nil)

func New(_ string) (*Provider, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: new client: %w", err)
	}
	return &Provider{cli: cli}, nil
}

func (p *Provider) Provision(ctx context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	if r.AgentPath == "" {
		return ports.Instance{}, fmt.Errorf("docker: AgentPath is required (agent side-load)")
	}
	env := make([]string, 0, len(r.Env))
	for k, v := range r.Env {
		env = append(env, k+"="+v)
	}

	mounts := []mount.Mount{
		{Type: mount.TypeBind, Source: r.AgentPath, Target: agentTarget, ReadOnly: true},
	}
	for _, m := range r.Mounts {
		mounts = append(mounts, mount.Mount{
			Type: mount.TypeBind, Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly,
		})
	}

	cfg := &container.Config{
		Image:      r.ImageRef,
		Env:        env,
		Entrypoint: []string{agentTarget},
		Labels:     map[string]string{labelWorkspace: r.WorkspaceID},
		Tty:        false,
	}
	host := &container.HostConfig{
		Mounts:     mounts,
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}
	if r.MemMB > 0 {
		host.Resources = container.Resources{Memory: r.MemMB * 1024 * 1024}
	}

	name := "mesa-" + r.WorkspaceID
	created, err := p.cli.ContainerCreate(ctx, cfg, host, nil, nil, name)
	if err != nil {
		return ports.Instance{}, fmt.Errorf("docker: create: %w", err)
	}
	if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return ports.Instance{}, fmt.Errorf("docker: start: %w", err)
	}
	return ports.Instance{Ref: created.ID, Phase: ports.InstanceRunning}, nil
}

func (p *Provider) Status(ctx context.Context, ref string) (ports.Instance, error) {
	info, err := p.cli.ContainerInspect(ctx, ref)
	if err != nil {
		if client.IsErrNotFound(err) {
			return ports.Instance{Ref: ref, Phase: ports.InstanceGone}, nil
		}
		return ports.Instance{}, fmt.Errorf("docker: inspect: %w", err)
	}
	phase := ports.InstanceStopped
	switch {
	case info.State.Running:
		phase = ports.InstanceRunning
	case strings.EqualFold(info.State.Status, "exited") && info.State.ExitCode != 0:
		phase = ports.InstanceFailed
	}
	return ports.Instance{Ref: ref, Phase: phase}, nil
}

func (p *Provider) Stop(ctx context.Context, ref string) error {
	if err := p.cli.ContainerStop(ctx, ref, container.StopOptions{}); err != nil {
		return fmt.Errorf("docker: stop: %w", err)
	}
	return nil
}

func (p *Provider) Destroy(ctx context.Context, ref string) error {
	err := p.cli.ContainerRemove(ctx, ref, container.RemoveOptions{Force: true})
	if err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("docker: remove: %w", err)
	}
	return nil
}
