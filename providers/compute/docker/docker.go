//go:build docker

// Package docker is the M1 Compute provider. It maps a neutral ProvisionRequest
// onto a Docker container that side-loads the hopbox-agent binary (bind-mounted,
// read-only) and runs it as the entrypoint. No Docker type crosses ports.*.
package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// hostPlatform pins workspace containers (and image pulls) to the host's arch.
// The hopbox-agent is a native binary injected and run as the entrypoint, so the
// workspace image's arch MUST match the agent's. Without pinning, a multi-arch
// image could resolve to a different arch than the injected agent (e.g. an
// amd64 image with an arm64 agent on Apple Silicon) and the agent fails to exec.
var hostPlatform = ocispec.Platform{OS: "linux", Architecture: runtime.GOARCH}

func platformString() string { return hostPlatform.OS + "/" + hostPlatform.Architecture }

const (
	labelWorkspace = "hopbox.workspace_id"
	agentTarget    = "/hopbox/hopbox-agent"
)

type Provider struct {
	cli       *client.Client
	network   string // dedicated bridge for workspace containers; "" = default bridge
	agentPort string // the agent hub port boxes are allowed to reach (from advertise)
}

var _ ports.Compute = (*Provider)(nil)

// Option configures the provider.
type Option func(*Provider)

// WithNetwork puts every workspace container on a dedicated bridge network
// (created on first use). Docker isolates separate bridges from one another, so
// boxes can no longer reach the host's other containers; the daemon also programs
// an egress firewall on the network's subnet (ensureFence) — no external script.
func WithNetwork(name string) Option { return func(p *Provider) { p.network = name } }

func New(advertise string, opts ...Option) (*Provider, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: new client: %w", err)
	}
	p := &Provider{cli: cli, agentPort: "7777"}
	if _, port, err := net.SplitHostPort(advertise); err == nil && port != "" {
		p.agentPort = port
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// workspaceSubnet is the fixed subnet of the dedicated workspace network. It is
// fixed (not docker-assigned) so the egress firewall can target it deterministically.
const workspaceSubnet = "172.31.0.0/24"

// ensureNetwork creates the dedicated workspace bridge (fixed subnet, no inter-
// container comms) if absent, and programs the egress firewall on it. Idempotent
// and re-run on every provision, so the fence self-heals and survives reboots /
// docker restarts — the daemon owns it, there is no script to run.
func (p *Provider) ensureNetwork(ctx context.Context) error {
	_, err := p.cli.NetworkInspect(ctx, p.network, network.InspectOptions{})
	if client.IsErrNotFound(err) {
		_, cerr := p.cli.NetworkCreate(ctx, p.network, network.CreateOptions{
			Driver: "bridge",
			IPAM:   &network.IPAM{Config: []network.IPAMConfig{{Subnet: workspaceSubnet}}},
			// Disable inter-container comms so one anonymous box can't reach another
			// on the same bridge.
			Options: map[string]string{"com.docker.network.bridge.enable_icc": "false"},
			Labels:  map[string]string{labelWorkspace: "network"},
		})
		if cerr != nil && !errdefsConflict(cerr) {
			return fmt.Errorf("docker: create network %q: %w", p.network, cerr)
		}
	} else if err != nil {
		return fmt.Errorf("docker: inspect network %q: %w", p.network, err)
	}
	p.ensureFence(workspaceSubnet, p.agentPort) // best-effort host egress firewall
	return nil
}

// errdefsConflict reports a 409 (network already exists) from a concurrent create.
func errdefsConflict(err error) bool { return strings.Contains(err.Error(), "already exists") }

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
	if r.MemMB > 0 || r.CPUMillis > 0 {
		host.Resources = container.Resources{}
		if r.MemMB > 0 {
			host.Resources.Memory = r.MemMB * 1024 * 1024
		}
		if r.CPUMillis > 0 {
			host.Resources.NanoCPUs = r.CPUMillis * 1_000_000 // milli-cores -> nano-cores
		}
	}
	// Put the box on the dedicated workspace bridge, isolating it from the host's
	// other containers (docker isolates separate bridges). host.docker.internal
	// still resolves to the host gateway here, so the agent reaches the hub.
	if p.network != "" {
		if err := p.ensureNetwork(ctx); err != nil {
			return ports.Instance{}, err
		}
		host.NetworkMode = container.NetworkMode(p.network)
	}

	name := "hopbox-" + r.WorkspaceID
	// Idempotency: a self-heal re-provision reuses the stable name while the
	// dead container may still exist in `exited` state holding it. Remove any
	// stale container of this name before create, else ContainerCreate fails
	// with a name conflict. Best-effort: ignore not-found.
	if err := p.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}); err != nil && !client.IsErrNotFound(err) {
		return ports.Instance{}, fmt.Errorf("docker: remove stale container %q: %w", name, err)
	}
	if err := p.ensureImage(ctx, r.ImageRef); err != nil {
		return ports.Instance{}, err
	}
	created, err := p.cli.ContainerCreate(ctx, cfg, host, nil, &hostPlatform, name)
	if err != nil {
		return ports.Instance{}, fmt.Errorf("docker: create: %w", err)
	}
	if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return ports.Instance{}, fmt.Errorf("docker: start: %w", err)
	}
	ip := ""
	if info, ierr := p.cli.ContainerInspect(ctx, created.ID); ierr == nil {
		ip = containerIP(info, p.network)
	}
	return ports.Instance{Ref: created.ID, Phase: ports.InstanceRunning, IP: ip}, nil
}

// containerIP returns the box's IP on its network (the dedicated bridge if set,
// else the default bridge) — the identity the metadata API matches by source IP.
func containerIP(info container.InspectResponse, network string) string {
	ns := info.NetworkSettings
	if ns == nil {
		return ""
	}
	if network != "" {
		if n, ok := ns.Networks[network]; ok && n != nil {
			return n.IPAddress
		}
	}
	return ns.IPAddress
}

// ensureImage pulls ref if it is not already present locally (IfNotPresent).
// The workspace image is user-supplied and frequently not cached; without this,
// ContainerCreate fails with "No such image". IfNotPresent (rather than always
// pulling) avoids re-pulling on every reconcile self-heal re-provision.
func (p *Provider) ensureImage(ctx context.Context, ref string) error {
	if _, err := p.cli.ImageInspect(ctx, ref); err == nil {
		return nil // already present
	} else if !client.IsErrNotFound(err) {
		return fmt.Errorf("docker: inspect image %q: %w", ref, err)
	}
	rc, err := p.cli.ImagePull(ctx, ref, image.PullOptions{Platform: platformString()})
	if err != nil {
		return fmt.Errorf("docker: pull image %q: %w", ref, err)
	}
	_, _ = io.Copy(io.Discard, rc) // drain to completion
	_ = rc.Close()
	return nil
}

// stageAgent makes the hopbox-agent binary available in the workspace as a Mount.
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
	rc, err := p.cli.ImagePull(ctx, a.ImageRef, image.PullOptions{Platform: platformString()})
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

	dir := target[:strings.LastIndex(target, "/")+1] // e.g. "/hopbox/"
	seed, err := p.cli.ContainerCreate(ctx, &container.Config{
		Image:      a.ImageRef,
		Entrypoint: []string{"cp", a.BinaryPath, target},
	}, &container.HostConfig{
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: volName, Target: dir}},
	}, nil, &hostPlatform, "")
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
