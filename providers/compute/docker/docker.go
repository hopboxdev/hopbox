//go:build docker

// Package docker is the M1 Compute provider. It maps a neutral ProvisionRequest
// onto a Docker container that side-loads the mesa-agent binary (bind-mounted,
// read-only) and runs it as the entrypoint. No Docker type crosses ports.*.
package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
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
	target := r.Agent.TargetPath
	if target == "" {
		target = agentTarget
	}
	env := make([]string, 0, len(r.Env))
	for k, v := range r.Env {
		env = append(env, k+"="+v)
	}

	agentMount, cleanup, err := p.stageAgent(ctx, r.Agent, target)
	if err != nil {
		return ports.Instance{}, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	mounts := []mount.Mount{agentMount}
	for _, m := range r.Mounts {
		mounts = append(mounts, mount.Mount{
			Type: mount.TypeBind, Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly,
		})
	}

	cfg := &container.Config{
		Image:      r.ImageRef,
		Env:        env,
		Entrypoint: []string{target},
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
	// Idempotency: a self-heal re-provision reuses the stable name while the
	// dead container may still exist in `exited` state holding it. Remove any
	// stale container of this name before create, else ContainerCreate fails
	// with a name conflict. Best-effort: ignore not-found.
	if err := p.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}); err != nil && !client.IsErrNotFound(err) {
		return ports.Instance{}, fmt.Errorf("docker: remove stale container %q: %w", name, err)
	}
	// TODO(arch): platform is nil — agent binary is linux/amd64; arch-mismatch on non-amd64 hosts is unhandled in M1.
	created, err := p.cli.ContainerCreate(ctx, cfg, host, nil, nil, name)
	if err != nil {
		return ports.Instance{}, fmt.Errorf("docker: create: %w", err)
	}
	if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return ports.Instance{}, fmt.Errorf("docker: start: %w", err)
	}
	return ports.Instance{Ref: created.ID, Phase: ports.InstanceRunning}, nil
}

// stageAgent makes the mesa-agent binary available in the workspace as a Mount.
// Fast path: bind-mount a host binary (dev). Otherwise: pull the agent image,
// run a throwaway container that copies the binary into a named volume, and
// mount that volume read-only at the target's directory.
func (p *Provider) stageAgent(ctx context.Context, a ports.AgentImage, target string) (mount.Mount, func(), error) {
	if a.HostBinaryPath != "" {
		return mount.Mount{Type: mount.TypeBind, Source: a.HostBinaryPath, Target: target, ReadOnly: true}, nil, nil
	}
	if a.ImageRef == "" || a.BinaryPath == "" {
		return mount.Mount{}, nil, fmt.Errorf("docker: Agent needs HostBinaryPath or (ImageRef+BinaryPath)")
	}
	rc, err := p.cli.ImagePull(ctx, a.ImageRef, image.PullOptions{})
	if err != nil {
		return mount.Mount{}, nil, fmt.Errorf("docker: pull agent image %q: %w", a.ImageRef, err)
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()

	vol, err := p.cli.VolumeCreate(ctx, volume.CreateOptions{})
	if err != nil {
		return mount.Mount{}, nil, fmt.Errorf("docker: create agent volume: %w", err)
	}
	volName := vol.Name

	dir := target[:strings.LastIndex(target, "/")+1] // e.g. "/mesa/"
	seed, err := p.cli.ContainerCreate(ctx, &container.Config{
		Image:      a.ImageRef,
		Entrypoint: []string{"cp", a.BinaryPath, target},
	}, &container.HostConfig{
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: volName, Target: dir}},
	}, nil, nil, "")
	if err != nil {
		return mount.Mount{}, nil, fmt.Errorf("docker: create agent seeder: %w", err)
	}
	if err := p.cli.ContainerStart(ctx, seed.ID, container.StartOptions{}); err != nil {
		return mount.Mount{}, nil, fmt.Errorf("docker: start agent seeder: %w", err)
	}
	statusCh, errCh := p.cli.ContainerWait(ctx, seed.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return mount.Mount{}, nil, fmt.Errorf("docker: wait agent seeder: %w", err)
		}
	case <-statusCh:
	}
	_ = p.cli.ContainerRemove(ctx, seed.ID, container.RemoveOptions{Force: true})

	cleanup := func() {} // volume is mounted into the workspace; reaped later
	return mount.Mount{Type: mount.TypeVolume, Source: volName, Target: dir, ReadOnly: true}, cleanup, nil
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
