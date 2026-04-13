package containers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/control"
	"github.com/hopboxdev/hopbox/internal/metrics"
)

const profileHashLabelKey = "hopbox.profile-hash"

func ContainerName(username, boxname string) string {
	return fmt.Sprintf("hopbox-%s-%s", username, boxname)
}

// ShouldRecreate returns true if the container's profile-hash label does not
// match the desired hash, indicating the container needs to be recreated.
func ShouldRecreate(containerLabel, wantHash string) bool {
	return containerLabel != wantHash
}

type containerState struct {
	sessions  int
	idleTimer *time.Timer
	user      string
	box       string
}

type Manager struct {
	cli         *client.Client
	sockets     map[string]*control.SocketServer // containerID -> socket server
	states      map[string]*containerState
	mu          sync.Mutex
	idleTimeout time.Duration
	resources   config.ResourcesConfig
	linkStore   *control.LinkStore
}

// SetLinkStore sets the LinkStore used for generating link codes in control sockets.
func (m *Manager) SetLinkStore(ls *control.LinkStore) {
	m.linkStore = ls
}

func NewManager(cli *client.Client, cfg config.Config) *Manager {
	var timeout time.Duration
	if cfg.IdleTimeoutHours > 0 {
		timeout = time.Duration(cfg.IdleTimeoutHours) * time.Hour
	}
	return &Manager{
		cli:         cli,
		sockets:     make(map[string]*control.SocketServer),
		states:      make(map[string]*containerState),
		idleTimeout: timeout,
		resources:   cfg.Resources,
	}
}

func (m *Manager) SessionConnect(containerID, user, box string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.states[containerID]
	if !ok {
		s = &containerState{user: user, box: box}
		m.states[containerID] = s
	}
	s.sessions++
	metrics.ActiveSessionsTotal.Inc()
	metrics.BoxActiveSessions.WithLabelValues(user, box, containerID).Inc()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
		slog.Info("idle timer cancelled (session reconnected)", "component", "idle", "container", containerID[:12])
	}
}

func (m *Manager) SessionDisconnect(containerID, user, box string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.states[containerID]
	if !ok {
		return
	}
	s.sessions--
	if s.sessions < 0 {
		s.sessions = 0
	}
	metrics.ActiveSessionsTotal.Dec()
	metrics.BoxActiveSessions.WithLabelValues(user, box, containerID).Dec()

	if s.sessions == 0 && m.idleTimeout > 0 {
		slog.Info("idle timer started", "component", "idle", "timeout", m.idleTimeout, "container", containerID[:12])
		s.idleTimer = time.AfterFunc(m.idleTimeout, func() {
			m.stopIdleContainer(containerID)
		})
	}
}

func (m *Manager) stopIdleContainer(containerID string) {
	slog.Info("stopping idle container", "component", "idle", "container", containerID[:12])
	ctx := context.Background()
	if err := m.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		slog.Error("failed to stop idle container", "component", "idle", "container", containerID[:12], "err", err)
	}
	// ContainersRunningTotal is refreshed by the metrics collector from
	// Docker directly — no manual bookkeeping here.

	m.mu.Lock()
	delete(m.states, containerID)
	m.mu.Unlock()
}

func (m *Manager) resourceLimits() container.Resources {
	var pidsLimit *int64
	if m.resources.PidsLimit > 0 {
		pl := m.resources.PidsLimit
		pidsLimit = &pl
	}

	r := container.Resources{
		PidsLimit: pidsLimit,
	}
	if m.resources.CPUCores > 0 {
		r.NanoCPUs = int64(m.resources.CPUCores) * 1_000_000_000
	}
	if m.resources.MemoryGB > 0 {
		r.Memory = int64(m.resources.MemoryGB) * 1024 * 1024 * 1024
	}
	return r
}

func (m *Manager) EnsureRunning(ctx context.Context, username, boxname, imageTag, profileHash, homePath string, info control.BoxInfo) (string, error) {
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
		if ShouldRecreate(c.Labels[profileHashLabelKey], profileHash) {
			slog.Info("profile hash mismatch, recreating container", "component", "container", "name", name)
			_ = m.cli.ContainerStop(ctx, c.ID, container.StopOptions{})
			if err := m.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
				return "", fmt.Errorf("remove container: %w", err)
			}
		} else {
			if c.State != "running" {
				if err := m.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
					return "", fmt.Errorf("start container: %w", err)
				}
			}

			// Ensure socket server is running for existing container
			m.mu.Lock()
			_, hasSocket := m.sockets[c.ID]
			m.mu.Unlock()
			if !hasSocket {
				socketPath := control.SocketPath(name)
				boxDir := filepath.Dir(homePath)
				info.ContainerID = c.ID
				info.StartedAt = time.Unix(c.Created, 0)
				destroyFn := func() error {
					return m.DestroyBox(context.Background(), username, boxname, boxDir)
				}
				srv, err := control.NewSocketServer(socketPath, info, destroyFn, m.linkStore)
				if err == nil {
					m.mu.Lock()
					m.sockets[c.ID] = srv
					m.mu.Unlock()
					go srv.Serve()
				}
			}

			return c.ID, nil
		}
	}

	containerName := ContainerName(username, boxname)
	socketDir := control.SocketDir(containerName)
	os.MkdirAll(socketDir, 0755)
	socketPath := control.SocketPath(containerName)

	cfg := &container.Config{
		Image:      imageTag,
		User:       "dev",
		WorkingDir: "/home/dev",
		Cmd:        []string{"sleep", "infinity"},
		Labels:     map[string]string{profileHashLabelKey: profileHash},
	}
	hostCfg := &container.HostConfig{
		Binds: []string{
			fmt.Sprintf("%s:/home/dev", homePath),
			fmt.Sprintf("%s:/var/run/hopbox", socketDir),
		},
		Resources: m.resourceLimits(),
	}

	slog.Info("creating container", "component", "container", "name", name, "home", homePath)

	resp, err := m.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := m.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	// Start control socket
	boxDir := filepath.Dir(homePath)
	info.ContainerID = resp.ID
	info.StartedAt = time.Now()
	destroyFn := func() error {
		return m.DestroyBox(context.Background(), username, boxname, boxDir)
	}
	srv, err := control.NewSocketServer(socketPath, info, destroyFn, m.linkStore)
	if err != nil {
		slog.Error("failed to create control socket", "component", "container", "err", err)
	} else {
		m.mu.Lock()
		m.sockets[resp.ID] = srv
		m.mu.Unlock()
		go srv.Serve()
	}

	return resp.ID, nil
}

func (m *Manager) DestroyBox(ctx context.Context, username, boxname, boxDir string) error {
	name := ContainerName(username, boxname)

	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	for _, c := range containers {
		m.mu.Lock()
		if srv, ok := m.sockets[c.ID]; ok {
			srv.Close()
			delete(m.sockets, c.ID)
		}
		m.mu.Unlock()

		_ = m.cli.ContainerStop(ctx, c.ID, container.StopOptions{})
		if err := m.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("remove container: %w", err)
		}
		// ContainersRunningTotal is refreshed by the metrics collector.
	}

	// Clean up socket directory
	socketDir := control.SocketDir(name)
	os.RemoveAll(socketDir)

	// Delete box directory
	if err := os.RemoveAll(boxDir); err != nil {
		return fmt.Errorf("remove box dir: %w", err)
	}

	return nil
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

// ExecNoTTY runs a command inside a container without allocating a TTY and
// returns its exit code. Used for non-interactive SSH sessions such as the
// VSCode remote-ssh bootstrap, scp, and rsync. stdout and stderr are
// demultiplexed from Docker's framed stream so the caller gets them on
// separate writers.
//
// Ownership of stdin is the caller's: after ExecNoTTY returns the stdin
// copy goroutine keeps running until the reader hits EOF. Callers must
// close their stdin source once the exec is done (ssh channel close,
// session teardown, etc.) to avoid leaking that goroutine.
func (m *Manager) ExecNoTTY(ctx context.Context, containerID string, cmd []string, env []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (int, error) {
	execCfg := container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		AttachStdin:  stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	}

	execResp, err := m.cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return -1, fmt.Errorf("exec create: %w", err)
	}

	attachResp, err := m.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{Tty: false})
	if err != nil {
		return -1, fmt.Errorf("exec attach: %w", err)
	}
	defer attachResp.Close()

	// stdin -> container; close the write side on EOF so the remote process
	// sees EOF on its own stdin (bootstrap scripts wait for this).
	if stdin != nil {
		go func() {
			_, _ = io.Copy(attachResp.Conn, stdin)
			_ = attachResp.CloseWrite()
		}()
	}

	// Demultiplex the framed stream into stdout and stderr. Blocks until
	// the exec process exits.
	if _, err := stdcopy.StdCopy(stdout, stderr, attachResp.Reader); err != nil {
		return -1, fmt.Errorf("stream copy: %w", err)
	}

	inspect, err := m.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return -1, fmt.Errorf("exec inspect: %w", err)
	}
	return inspect.ExitCode, nil
}

// Shutdown cleans up all socket servers and cancels all idle timers.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, srv := range m.sockets {
		slog.Info("closing socket server", "component", "shutdown", "container", id[:12])
		srv.Close()
	}
	m.sockets = make(map[string]*control.SocketServer)

	for id, s := range m.states {
		if s.idleTimer != nil {
			s.idleTimer.Stop()
			slog.Info("cancelled idle timer", "component", "shutdown", "container", id[:12])
		}
	}
	m.states = make(map[string]*containerState)
}

// ListBoxes returns the names of all box directories under the given user directory.
func ListBoxes(userDir string) ([]string, error) {
	boxesDir := filepath.Join(userDir, "boxes")
	entries, err := os.ReadDir(boxesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var boxes []string
	for _, e := range entries {
		if e.IsDir() {
			boxes = append(boxes, e.Name())
		}
	}
	return boxes, nil
}
