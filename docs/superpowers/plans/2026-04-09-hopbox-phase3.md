# Hopbox Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a box picker TUI (`hop+?`), a per-container Unix socket for control commands, and an in-container `hopbox` CLI with `status` and `destroy` commands.

**Architecture:** The picker is a huh Select rendered via the same `runProgram()` helper used by the wizard. The control socket is a Unix socket created per container on the host and bind-mounted into the container. hopboxd listens on each socket with a goroutine. The `hopbox` CLI is a small static Go binary baked into the base image that talks to `/var/run/hopbox.sock`.

**Tech Stack:** Go, charmbracelet/huh, charmbracelet/ssh, Docker SDK, net (Unix sockets), encoding/json

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/control/protocol.go` | Shared request/response types (used by both hopboxd and CLI) |
| `internal/control/protocol_test.go` | Protocol serialization tests |
| `internal/control/socket.go` | Per-container socket server: listen, accept, dispatch to handler |
| `internal/control/handler.go` | Command handlers: status, destroy |
| `internal/picker/picker.go` | Picker TUI: list boxes, select one |
| `cmd/hopbox/main.go` | In-container CLI binary |
| `scripts/build-cli.sh` | Cross-compile CLI for linux |
| `internal/containers/manager.go` | Modified: socket mount, destroy method, list boxes |
| `internal/gateway/server.go` | Modified: detect `?` boxname → picker, pass control socket info |
| `templates/Dockerfile.base` | Modified: COPY hopbox CLI binary |

---

### Task 1: Control Protocol Types

**Files:**
- Create: `internal/control/protocol.go`
- Create: `internal/control/protocol_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/control/protocol_test.go`:

```go
package control

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshal(t *testing.T) {
	req := Request{Command: "status"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Command != "status" {
		t.Errorf("command: got %q, want %q", decoded.Command, "status")
	}
}

func TestRequestDestroy(t *testing.T) {
	req := Request{Command: "destroy", Confirm: "mybox"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Confirm != "mybox" {
		t.Errorf("confirm: got %q, want %q", decoded.Confirm, "mybox")
	}
}

func TestResponseOK(t *testing.T) {
	resp := Response{OK: true, Data: map[string]string{"box": "default"}}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK {
		t.Error("expected OK=true")
	}
	if decoded.Data["box"] != "default" {
		t.Errorf("data.box: got %q, want %q", decoded.Data["box"], "default")
	}
}

func TestResponseError(t *testing.T) {
	resp := Response{OK: false, Error: "not found"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OK {
		t.Error("expected OK=false")
	}
	if decoded.Error != "not found" {
		t.Errorf("error: got %q, want %q", decoded.Error, "not found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/control/ -v
```

Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement protocol.go**

Create `internal/control/protocol.go`:

```go
package control

// Request is sent from the in-container CLI to hopboxd via the Unix socket.
type Request struct {
	Command string `json:"command"`          // "status" | "destroy"
	Confirm string `json:"confirm,omitempty"` // box name confirmation for destroy
}

// Response is sent from hopboxd back to the CLI.
type Response struct {
	OK    bool              `json:"ok"`
	Data  map[string]string `json:"data,omitempty"`
	Error string            `json:"error,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/control/ -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/control/
git commit -m "feat: add control socket protocol types"
```

---

### Task 2: Control Socket Server

**Files:**
- Create: `internal/control/socket.go`
- Create: `internal/control/handler.go`

- [ ] **Step 1: Implement handler.go**

Create `internal/control/handler.go`:

```go
package control

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

// BoxInfo holds metadata about a box for the status command.
type BoxInfo struct {
	BoxName     string
	Username    string
	OS          string
	Arch        string
	Shell       string
	Multiplexer string
	ContainerID string
	StartedAt   time.Time
}

// DestroyFunc is called by the destroy handler to clean up the container and box data.
type DestroyFunc func() error

// HandleRequest processes a control request and returns a response.
func HandleRequest(req Request, info BoxInfo, destroyFn DestroyFunc) Response {
	switch req.Command {
	case "status":
		return handleStatus(info)
	case "destroy":
		return handleDestroy(req, info, destroyFn)
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}

func handleStatus(info BoxInfo) Response {
	uptime := time.Since(info.StartedAt).Truncate(time.Second).String()

	return Response{
		OK: true,
		Data: map[string]string{
			"box":         info.BoxName,
			"user":        info.Username,
			"os":          fmt.Sprintf("Ubuntu 24.04 (%s)", runtime.GOARCH),
			"shell":       info.Shell,
			"multiplexer": info.Multiplexer,
			"uptime":      uptime,
		},
	}
}

func handleDestroy(req Request, info BoxInfo, destroyFn DestroyFunc) Response {
	if req.Confirm != info.BoxName {
		return Response{OK: false, Error: fmt.Sprintf("confirmation does not match box name %q", info.BoxName)}
	}

	log.Printf("[control] destroying box %s (container %s)", info.BoxName, info.ContainerID[:12])
	if err := destroyFn(); err != nil {
		return Response{OK: false, Error: fmt.Sprintf("destroy: %v", err)}
	}

	return Response{OK: true, Data: map[string]string{"destroyed": info.BoxName}}
}
```

- [ ] **Step 2: Implement socket.go**

Create `internal/control/socket.go`:

```go
package control

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"sync"
)

// SocketServer listens on a Unix socket and dispatches control commands.
type SocketServer struct {
	path      string
	listener  net.Listener
	info      BoxInfo
	destroyFn DestroyFunc
	done      chan struct{}
	wg        sync.WaitGroup
}

// NewSocketServer creates a socket server for a container.
func NewSocketServer(socketPath string, info BoxInfo, destroyFn DestroyFunc) (*SocketServer, error) {
	// Remove stale socket file
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	// Make socket world-writable so the dev user inside the container can access it
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		return nil, err
	}

	return &SocketServer{
		path:      socketPath,
		listener:  listener,
		info:      info,
		destroyFn: destroyFn,
		done:      make(chan struct{}),
	}, nil
}

// Serve starts accepting connections. Blocks until Close is called.
func (s *SocketServer) Serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return // intentional close
			default:
				log.Printf("[control] accept error on %s: %v", s.path, err)
				return
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *SocketServer) handleConn(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		resp := Response{OK: false, Error: "invalid request"}
		json.NewEncoder(conn).Encode(resp)
		return
	}

	resp := HandleRequest(req, s.info, s.destroyFn)
	json.NewEncoder(conn).Encode(resp)
}

// Close stops the socket server and cleans up.
func (s *SocketServer) Close() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
	os.Remove(s.path)
}

// SocketPath returns the path of the Unix socket on the host.
func SocketPath(containerName string) string {
	return "/tmp/hopbox-" + containerName + ".sock"
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/control/
```

Expected: compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/control/
git commit -m "feat: add control socket server with status and destroy handlers"
```

---

### Task 3: In-Container CLI

**Files:**
- Create: `cmd/hopbox/main.go`
- Create: `scripts/build-cli.sh`

- [ ] **Step 1: Implement the CLI**

Create `cmd/hopbox/main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/hopboxdev/hopbox/internal/control"
)

const socketPath = "/var/run/hopbox.sock"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "status":
		jsonOutput := len(os.Args) > 2 && os.Args[2] == "--json"
		doStatus(jsonOutput)
	case "destroy":
		doDestroy()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: hopbox <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  status [--json]  Show box info")
	fmt.Fprintln(os.Stderr, "  destroy          Destroy this box")
}

func sendRequest(req control.Request) (control.Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return control.Response{}, fmt.Errorf("connect to hopboxd: %w (is hopboxd running?)", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return control.Response{}, fmt.Errorf("send request: %w", err)
	}

	var resp control.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return control.Response{}, fmt.Errorf("read response: %w", err)
	}
	return resp, nil
}

func doStatus(jsonOutput bool) {
	resp, err := sendRequest(control.Request{Command: "status"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	if jsonOutput {
		data, _ := json.Marshal(resp.Data)
		fmt.Println(string(data))
		return
	}

	fmt.Printf("Box:         %s\n", resp.Data["box"])
	fmt.Printf("User:        %s\n", resp.Data["user"])
	fmt.Printf("OS:          %s\n", resp.Data["os"])
	fmt.Printf("Shell:       %s\n", resp.Data["shell"])
	fmt.Printf("Multiplexer: %s\n", resp.Data["multiplexer"])
	fmt.Printf("Uptime:      %s\n", resp.Data["uptime"])
}

func doDestroy() {
	// First get status to know the box name
	statusResp, err := sendRequest(control.Request{Command: "status"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !statusResp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", statusResp.Error)
		os.Exit(1)
	}

	boxName := statusResp.Data["box"]

	fmt.Printf("Are you sure you want to destroy box %q? This will:\n", boxName)
	fmt.Println("  - Stop and remove this container")
	fmt.Println("  - Delete the home directory for this box")
	fmt.Println()
	fmt.Printf("Type the box name to confirm: ")

	var confirm string
	fmt.Scanln(&confirm)

	if confirm != boxName {
		fmt.Println("Aborted.")
		os.Exit(1)
	}

	resp, err := sendRequest(control.Request{Command: "destroy", Confirm: confirm})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	fmt.Println("Destroying... done.")
}
```

- [ ] **Step 2: Create the build script**

Create `scripts/build-cli.sh`:

```bash
#!/bin/bash
set -euo pipefail

# Cross-compile the hopbox CLI for linux
# Uses the host's GOARCH to match the Docker container architecture

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 \
    go build -o "$PROJECT_ROOT/templates/hopbox" "$PROJECT_ROOT/cmd/hopbox/"

echo "Built templates/hopbox for linux/$(go env GOARCH)"
```

```bash
chmod +x scripts/build-cli.sh
```

- [ ] **Step 3: Build the CLI and verify**

```bash
./scripts/build-cli.sh
file templates/hopbox
```

Expected: `templates/hopbox: ELF 64-bit LSB executable, ARM aarch64` (or x86-64 depending on host).

- [ ] **Step 4: Commit**

```bash
git add cmd/hopbox/ scripts/build-cli.sh
git commit -m "feat: add in-container hopbox CLI with status and destroy commands"
```

---

### Task 4: Update Base Image with CLI

**Files:**
- Modify: `templates/Dockerfile.base`
- Modify: `.gitignore`

- [ ] **Step 1: Add CLI binary to Dockerfile.base**

Update `templates/Dockerfile.base` — add after the mise activation line:

```dockerfile
# In-container hopbox CLI
COPY hopbox /usr/local/bin/hopbox
```

The full Dockerfile.base becomes:

```dockerfile
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    sudo curl wget git build-essential openssh-client \
    unzip xz-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create dev user (remove existing UID 1000 user if present, e.g. ubuntu)
RUN existing=$(getent passwd 1000 | cut -d: -f1) && \
    if [ -n "$existing" ]; then userdel -r "$existing"; fi && \
    useradd -m -s /bin/bash -u 1000 dev && \
    echo "dev ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers.d/dev

# Install mise (runtime version manager)
RUN curl https://mise.run | sh \
    && mv /root/.local/bin/mise /usr/local/bin/mise

# Set up mise data outside /home/dev (which gets bind-mounted)
ENV MISE_DATA_DIR=/opt/mise
ENV MISE_CONFIG_DIR=/opt/mise/config
RUN mkdir -p /opt/mise/config && chown -R dev:dev /opt/mise

# Activate mise in all bash sessions
RUN echo 'eval "$(/usr/local/bin/mise activate bash)"' >> /etc/bash.bashrc

# In-container hopbox CLI
COPY hopbox /usr/local/bin/hopbox

USER dev
WORKDIR /home/dev

CMD ["sleep", "infinity"]
```

- [ ] **Step 2: Add templates/hopbox to .gitignore**

Add `templates/hopbox` to `.gitignore` (it's a compiled binary that gets built by the script).

- [ ] **Step 3: Build CLI and verify base image builds**

```bash
./scripts/build-cli.sh
go build -o hopboxd ./cmd/hopboxd/ && ./hopboxd
```

hopboxd will rebuild the base image (template hash changed). Verify the base image builds successfully.

- [ ] **Step 4: Commit**

```bash
git add templates/Dockerfile.base .gitignore
git commit -m "feat: add hopbox CLI binary to base image"
```

---

### Task 5: Wire Control Socket into Container Manager

**Files:**
- Modify: `internal/containers/manager.go`

- [ ] **Step 1: Add socket mount and tracking to Manager**

Update `Manager` struct to track active socket servers:

```go
type Manager struct {
	cli     *client.Client
	sockets map[string]*control.SocketServer // containerID -> socket server
	mu      sync.Mutex
}

func NewManager(cli *client.Client) *Manager {
	return &Manager{
		cli:     cli,
		sockets: make(map[string]*control.SocketServer),
	}
}
```

Add import for `"sync"` and `"github.com/hopboxdev/hopbox/internal/control"`.

- [ ] **Step 2: Update EnsureRunning to mount socket**

Change `EnsureRunning` signature to accept `BoxInfo` for the socket server:

```go
func (m *Manager) EnsureRunning(ctx context.Context, username, boxname, imageTag, profileHash, homePath string, info control.BoxInfo) (string, error) {
```

In the container creation section, add the socket mount to `Binds`:

```go
	socketPath := control.SocketPath(ContainerName(username, boxname))
	
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
			fmt.Sprintf("%s:/var/run/hopbox.sock", socketPath),
		},
	}
```

After starting the container, create and start the socket server:

```go
	// Start control socket
	boxDir := filepath.Dir(homePath) // boxes/<boxname>/
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
```

Also start socket server for existing containers that are reused:

```go
	if len(containers) > 0 {
		c := containers[0]
		// ... existing profile hash check and recreation ...
		
		// For existing running containers, ensure socket server is running
		m.mu.Lock()
		_, hasSocket := m.sockets[c.ID]
		m.mu.Unlock()
		if !hasSocket {
			socketPath := control.SocketPath(name)
			info.ContainerID = c.ID
			info.StartedAt = time.Unix(c.Created, 0)
			boxDir := filepath.Dir(homePath)
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
	}
```

- [ ] **Step 3: Add Destroy method**

```go
// DestroyBox stops a container, removes it, cleans up socket, and deletes box data.
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
		// Clean up socket server
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

	// Delete box directory
	if err := os.RemoveAll(boxDir); err != nil {
		return fmt.Errorf("remove box dir: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Add ListBoxes helper**

```go
// ListBoxes returns the box names for a user by scanning their boxes directory.
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
```

Note: Add this to `internal/containers/manager.go` as a package-level function (doesn't need the Manager receiver since it just reads the filesystem).

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/containers/
```

Expected: compiles. The gateway package will break (EnsureRunning signature changed) — Task 7 fixes that.

- [ ] **Step 6: Commit**

```bash
git add internal/containers/manager.go
git commit -m "feat: wire control socket into container lifecycle with destroy support"
```

---

### Task 6: Picker TUI

**Files:**
- Create: `internal/picker/picker.go`

- [ ] **Step 1: Implement the picker**

Create `internal/picker/picker.go`:

```go
package picker

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"
)

// RunPicker shows a box selection TUI and returns the chosen box name.
func RunPicker(boxes []string, sess ssh.Session) (string, error) {
	if len(boxes) == 0 {
		return "", fmt.Errorf("no boxes found")
	}

	pty, winCh, ok := sess.Pty()
	if !ok {
		return "", fmt.Errorf("no PTY available")
	}

	var selected string
	options := make([]huh.Option[string], len(boxes))
	for i, box := range boxes {
		options[i] = huh.NewOption(box, box)
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Select a box").
			Options(options...).
			Value(&selected),
	))

	p := tea.NewProgram(form, bubbletea.MakeOptions(sess)...)

	go func() {
		p.Send(tea.WindowSizeMsg{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		})
	}()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case w, ok := <-winCh:
				if !ok {
					return
				}
				p.Send(tea.WindowSizeMsg{
					Width:  w.Width,
					Height: w.Height,
				})
			}
		}
	}()

	result, err := p.Run()
	close(done)
	if err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}

	f := result.(*huh.Form)
	if f.State == huh.StateAborted {
		return "", fmt.Errorf("picker cancelled")
	}

	return selected, nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/picker/
```

- [ ] **Step 3: Commit**

```bash
git add internal/picker/
git commit -m "feat: add box picker TUI for hop+? selection"
```

---

### Task 7: Wire Picker and Socket into Session Handler

**Files:**
- Modify: `internal/gateway/server.go`
- Modify: `internal/gateway/tunnel.go`

- [ ] **Step 1: Update server.go imports**

Add:
```go
"github.com/hopboxdev/hopbox/internal/control"
"github.com/hopboxdev/hopbox/internal/picker"
```

- [ ] **Step 2: Add picker logic at the start of sessionHandler**

After parsing the boxname, add:

```go
	// Handle picker request (hop+?)
	if boxname == "?" {
		if needsReg {
			fmt.Fprintf(sess, "Please register first: ssh hop@server\r\n")
			sess.Exit(1)
			return
		}

		user, ok := s.store.LookupByFingerprint(fp)
		if !ok {
			fmt.Fprintf(sess, "User not found\r\n")
			return
		}

		userDir := filepath.Join(s.store.Dir(), fp)
		boxes, err := containers.ListBoxes(userDir)
		if err != nil {
			fmt.Fprintf(sess, "Failed to list boxes: %v\r\n", err)
			return
		}

		if len(boxes) == 0 {
			// No boxes yet — redirect to default box which will trigger wizard
			boxname = "default"
		} else {
			chosen, err := picker.RunPicker(boxes, sess)
			if err != nil {
				log.Printf("[session] picker cancelled for user=%s: %v", user.Username, err)
				sess.Exit(0)
				return
			}
			boxname = chosen
			log.Printf("[session] user=%s picked box=%s", user.Username, boxname)
		}
	}
```

- [ ] **Step 3: Update EnsureRunning call to pass BoxInfo**

Build the `BoxInfo` before calling `EnsureRunning`:

```go
	boxInfo := control.BoxInfo{
		BoxName:     boxname,
		Username:    user.Username,
		Shell:       profile.Shell.Tool,
		Multiplexer: profile.Multiplexer.Tool,
	}
	containerID, err := s.manager.EnsureRunning(ctx, user.Username, boxname, imageTag, profileHash, homePath, boxInfo)
```

- [ ] **Step 4: Update tunnel.go EnsureRunning call**

In `resolveContainerID`, update the `EnsureRunning` call to include a default `BoxInfo`:

```go
	boxInfo := control.BoxInfo{
		BoxName:  boxname,
		Username: user.Username,
	}
	if profile != nil {
		boxInfo.Shell = profile.Shell.Tool
		boxInfo.Multiplexer = profile.Multiplexer.Tool
	}
	containerID, err := mgr.EnsureRunning(context.Background(), user.Username, boxname, imageTag, profileHash, homePath, boxInfo)
```

Add `"github.com/hopboxdev/hopbox/internal/control"` to tunnel.go imports.

- [ ] **Step 5: Verify the whole project compiles**

```bash
go build ./...
```

- [ ] **Step 6: Run all tests**

```bash
go test ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/gateway/server.go internal/gateway/tunnel.go
git commit -m "feat: wire picker TUI and control socket into session handler"
```

---

### Task 8: Integration Smoke Test

**Files:** None (manual testing)

- [ ] **Step 1: Build CLI and rebuild**

```bash
./scripts/build-cli.sh
/usr/local/bin/docker rm -f $(/usr/local/bin/docker ps -aq --filter "name=hopbox-") 2>/dev/null
rm -rf data/users/
go build -o hopboxd ./cmd/hopboxd/ && ./hopboxd
```

Base image will rebuild (new CLI binary + template hash change).

- [ ] **Step 2: Test normal flow**

```bash
ssh -p 2222 hop@localhost
```

Register, complete wizard, land in container. Inside the container:

```bash
hopbox status
hopbox status --json
```

- [ ] **Step 3: Test picker**

Create a second box first:

```bash
ssh -p 2222 hop+project1@localhost
```

Complete wizard, exit. Then test picker:

```bash
ssh -p 2222 hop+?@localhost
```

Should show a picker with `default` and `project1`.

- [ ] **Step 4: Test destroy**

Inside a container:

```bash
hopbox destroy
```

Should prompt for box name, then destroy and disconnect.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix: address issues found during Phase 3 integration testing"
```

---

## Task Dependency Graph

```
Task 1 (Protocol) ──────────────────────┐
Task 2 (Socket Server + Handler) ────────┤
Task 3 (CLI Binary) ─────────────────────┤
Task 4 (Base Image + CLI) ───────────────┼──► Task 7 (Wire Session Handler) ──► Task 8 (Smoke Test)
Task 5 (Manager: Socket + Destroy) ──────┤
Task 6 (Picker TUI) ─────────────────────┘
```

Tasks 1-6 are mostly independent (Task 2 depends on Task 1's types, Task 5 depends on Task 2). Task 7 wires everything together. Task 8 is the final verification.
