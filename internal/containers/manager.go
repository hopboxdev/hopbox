package containers

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/control"
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
}

type Manager struct {
	cli         *client.Client
	sockets     map[string]*control.SocketServer // containerID -> socket server
	states      map[string]*containerState
	mu          sync.Mutex
	idleTimeout time.Duration
	resources   config.ResourcesConfig
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

func (m *Manager) SessionConnect(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.states[containerID]
	if !ok {
		s = &containerState{}
		m.states[containerID] = s
	}
	s.sessions++

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
		log.Printf("[idle] cancelled idle timer for container %s (session reconnected)", containerID[:12])
	}
}

func (m *Manager) SessionDisconnect(containerID string) {
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

	if s.sessions == 0 && m.idleTimeout > 0 {
		log.Printf("[idle] starting %v idle timer for container %s", m.idleTimeout, containerID[:12])
		s.idleTimer = time.AfterFunc(m.idleTimeout, func() {
			m.stopIdleContainer(containerID)
		})
	}
}

func (m *Manager) stopIdleContainer(containerID string) {
	log.Printf("[idle] stopping idle container %s", containerID[:12])
	ctx := context.Background()
	if err := m.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		log.Printf("[idle] failed to stop container %s: %v", containerID[:12], err)
	}

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
			log.Printf("[container] profile hash mismatch for %s — removing and recreating", name)
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
				srv, err := control.NewSocketServer(socketPath, info, destroyFn)
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

	log.Printf("[container] creating %s with bind mount %s -> /home/dev", name, homePath)

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
	srv, err := control.NewSocketServer(socketPath, info, destroyFn)
	if err != nil {
		log.Printf("[container] failed to create control socket: %v", err)
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

// Shutdown cleans up all socket servers and cancels all idle timers.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, srv := range m.sockets {
		log.Printf("[shutdown] closing socket server for container %s", id[:12])
		srv.Close()
	}
	m.sockets = make(map[string]*control.SocketServer)

	for id, s := range m.states {
		if s.idleTimer != nil {
			s.idleTimer.Stop()
			log.Printf("[shutdown] cancelled idle timer for container %s", id[:12])
		}
	}
	m.states = make(map[string]*containerState)
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
