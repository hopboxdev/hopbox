# hopbox-hostd Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a gRPC daemon that wraps the silo VM library to expose workspace lifecycle operations, replacing the manual poc-managed workflow.

**Architecture:** A `hopbox-hostd` binary in the hopbox repo that imports silo as a Go module. One gRPC service (`HostService`) with 7 RPCs. The `CreateWorkspace` RPC does the full provisioning flow: create VM, inject hop-agent, seed entropy, exchange WireGuard keys, start hop-agent, set up iptables port forwarding. Port allocator assigns unique UDP ports per workspace. Localhost-only binding for now.

**Tech Stack:** Go, gRPC/protobuf (buf for codegen), silo library (github.com/hopboxdev/silo), iptables

---

### Task 1: Set up buf and protobuf tooling

**Files:**
- Create: `buf.yaml`
- Create: `buf.gen.yaml`

**Step 1: Create buf.yaml**

```yaml
# buf.yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

**Step 2: Create buf.gen.yaml**

```yaml
# buf.gen.yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - remote: buf.build/grpc/go
    out: gen
    opt: paths=source_relative
```

**Step 3: Verify buf is working**

Run: `cd /Users/gandalfledev/Developer/hopbox && buf --version`
Expected: version output (already installed at /Users/gandalfledev/go/bin/buf)

**Step 4: Commit**

```bash
git add buf.yaml buf.gen.yaml
git commit -m "build: add buf config for protobuf generation"
```

---

### Task 2: Define the protobuf service

**Files:**
- Create: `proto/hostd/v1/hostd.proto`

**Step 1: Write the proto file**

```protobuf
syntax = "proto3";

package hostd.v1;

option go_package = "github.com/hopboxdev/hopbox/gen/hostd/v1;hostdv1";

service HostService {
  rpc CreateWorkspace(CreateWorkspaceRequest) returns (CreateWorkspaceResponse);
  rpc DestroyWorkspace(DestroyWorkspaceRequest) returns (DestroyWorkspaceResponse);
  rpc SuspendWorkspace(SuspendWorkspaceRequest) returns (SuspendWorkspaceResponse);
  rpc ResumeWorkspace(ResumeWorkspaceRequest) returns (ResumeWorkspaceResponse);
  rpc GetWorkspace(GetWorkspaceRequest) returns (GetWorkspaceResponse);
  rpc ListWorkspaces(ListWorkspacesRequest) returns (ListWorkspacesResponse);
  rpc HostStatus(HostStatusRequest) returns (HostStatusResponse);
}

// --- CreateWorkspace ---

message CreateWorkspaceRequest {
  string name = 1;
  string image = 2;       // base image name (e.g. "ubuntu-dev")
  int32 vcpus = 3;        // default: 2
  int32 memory_mb = 4;    // default: 2048
  int32 disk_gb = 5;      // default: 10
}

message CreateWorkspaceResponse {
  WorkspaceInfo workspace = 1;
  ClientConfig client_config = 2;
}

// Client config for hop CLI to connect
message ClientConfig {
  string name = 1;
  string endpoint = 2;          // "host:port"
  string private_key = 3;       // base64 client WireGuard private key
  string peer_public_key = 4;   // base64 server WireGuard public key
  string tunnel_ip = 5;         // "10.10.0.1/24"
  string agent_ip = 6;          // "10.10.0.2"
}

// --- DestroyWorkspace ---

message DestroyWorkspaceRequest {
  string name = 1;
}

message DestroyWorkspaceResponse {}

// --- SuspendWorkspace ---

message SuspendWorkspaceRequest {
  string name = 1;
}

message SuspendWorkspaceResponse {}

// --- ResumeWorkspace ---

message ResumeWorkspaceRequest {
  string name = 1;
}

message ResumeWorkspaceResponse {
  WorkspaceInfo workspace = 1;
}

// --- GetWorkspace ---

message GetWorkspaceRequest {
  string name = 1;
}

message GetWorkspaceResponse {
  WorkspaceInfo workspace = 1;
}

// --- ListWorkspaces ---

message ListWorkspacesRequest {}

message ListWorkspacesResponse {
  repeated WorkspaceInfo workspaces = 1;
}

// --- HostStatus ---

message HostStatusRequest {}

message HostStatusResponse {
  int32 total_vcpus = 1;
  int32 available_vcpus = 2;
  int64 total_memory_mb = 3;
  int64 available_memory_mb = 4;
  int64 total_disk_gb = 5;
  int64 available_disk_gb = 6;
  int32 running_workspaces = 7;
  int32 suspended_workspaces = 8;
}

// --- Shared ---

message WorkspaceInfo {
  string name = 1;
  string state = 2;        // "created", "running", "stopped", "suspended"
  int32 vcpus = 3;
  int32 memory_mb = 4;
  int32 disk_gb = 5;
  int32 host_port = 6;     // UDP port on host for WireGuard
  string vm_ip = 7;        // TAP IP of VM (internal)
}
```

**Step 2: Generate Go code**

Run: `cd /Users/gandalfledev/Developer/hopbox && buf generate`
Expected: Creates `gen/hostd/v1/hostd.pb.go` and `gen/hostd/v1/hostd_grpc.pb.go`

**Step 3: Verify generated files exist**

Run: `ls gen/hostd/v1/`
Expected: `hostd.pb.go  hostd_grpc.pb.go`

**Step 4: Add gRPC dependencies to go.mod**

Run: `cd /Users/gandalfledev/Developer/hopbox && go get google.golang.org/grpc google.golang.org/protobuf`

**Step 5: Verify it compiles**

Run: `go build ./gen/...`
Expected: no errors

**Step 6: Commit**

```bash
git add proto/ gen/ go.mod go.sum
git commit -m "feat: define hostd gRPC service and generate Go code"
```

---

### Task 3: Port allocator

**Files:**
- Create: `internal/hostd/portalloc.go`
- Create: `internal/hostd/portalloc_test.go`

**Step 1: Write the failing tests**

```go
// internal/hostd/portalloc_test.go
package hostd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortAllocator_Allocate(t *testing.T) {
	dir := t.TempDir()
	pa, err := NewPortAllocator(51820, 51830, filepath.Join(dir, "ports.json"))
	if err != nil {
		t.Fatal(err)
	}

	port, err := pa.Allocate("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if port < 51820 || port > 51830 {
		t.Fatalf("port %d out of range", port)
	}
}

func TestPortAllocator_Release(t *testing.T) {
	dir := t.TempDir()
	pa, err := NewPortAllocator(51820, 51830, filepath.Join(dir, "ports.json"))
	if err != nil {
		t.Fatal(err)
	}

	port, _ := pa.Allocate("ws-1")
	if err := pa.Release("ws-1"); err != nil {
		t.Fatal(err)
	}

	// Same port should be available again
	port2, _ := pa.Allocate("ws-2")
	if port2 != port {
		t.Fatalf("expected reuse of port %d, got %d", port, port2)
	}
}

func TestPortAllocator_Exhaustion(t *testing.T) {
	dir := t.TempDir()
	pa, err := NewPortAllocator(51820, 51822, filepath.Join(dir, "ports.json"))
	if err != nil {
		t.Fatal(err)
	}

	pa.Allocate("ws-1")
	pa.Allocate("ws-2")
	pa.Allocate("ws-3")

	_, err = pa.Allocate("ws-4")
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestPortAllocator_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ports.json")

	pa1, _ := NewPortAllocator(51820, 51830, path)
	pa1.Allocate("ws-1")
	port1, _ := pa1.Get("ws-1")

	// Reload from disk
	pa2, _ := NewPortAllocator(51820, 51830, path)
	port2, err := pa2.Get("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if port1 != port2 {
		t.Fatalf("port not persisted: got %d, want %d", port2, port1)
	}
}

func TestPortAllocator_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	pa, _ := NewPortAllocator(51820, 51830, filepath.Join(dir, "ports.json"))

	pa.Allocate("ws-1")
	_, err := pa.Allocate("ws-1")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/gandalfledev/Developer/hopbox && go test ./internal/hostd/ -v -run TestPortAllocator`
Expected: FAIL (package doesn't exist yet)

**Step 3: Write the implementation**

```go
// internal/hostd/portalloc.go
package hostd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PortAllocator manages UDP port assignments for workspaces.
// Assignments are persisted to a JSON file so they survive daemon restarts.
type PortAllocator struct {
	mu       sync.Mutex
	minPort  int
	maxPort  int
	path     string                // persistence file path
	byName   map[string]int        // workspace name → port
	byPort   map[int]string        // port → workspace name
}

// NewPortAllocator creates a port allocator for the range [minPort, maxPort].
// If the persistence file exists, it loads previous assignments.
func NewPortAllocator(minPort, maxPort int, path string) (*PortAllocator, error) {
	pa := &PortAllocator{
		minPort: minPort,
		maxPort: maxPort,
		path:    path,
		byName:  make(map[string]int),
		byPort:  make(map[int]string),
	}
	if err := pa.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading port allocations: %w", err)
	}
	return pa, nil
}

// Allocate assigns the next available port to the named workspace.
func (pa *PortAllocator) Allocate(name string) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if _, exists := pa.byName[name]; exists {
		return 0, fmt.Errorf("workspace %q already has a port allocated", name)
	}

	for port := pa.minPort; port <= pa.maxPort; port++ {
		if _, used := pa.byPort[port]; !used {
			pa.byName[name] = port
			pa.byPort[port] = name
			if err := pa.save(); err != nil {
				delete(pa.byName, name)
				delete(pa.byPort, port)
				return 0, fmt.Errorf("persisting allocation: %w", err)
			}
			return port, nil
		}
	}

	return 0, fmt.Errorf("no ports available in range %d-%d", pa.minPort, pa.maxPort)
}

// Release frees the port assigned to the named workspace.
func (pa *PortAllocator) Release(name string) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, exists := pa.byName[name]
	if !exists {
		return nil // idempotent
	}

	delete(pa.byName, name)
	delete(pa.byPort, port)
	return pa.save()
}

// Get returns the port assigned to the named workspace.
func (pa *PortAllocator) Get(name string) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, exists := pa.byName[name]
	if !exists {
		return 0, fmt.Errorf("no port allocated for workspace %q", name)
	}
	return port, nil
}

func (pa *PortAllocator) load() error {
	data, err := os.ReadFile(pa.path)
	if err != nil {
		return err
	}
	var assignments map[string]int
	if err := json.Unmarshal(data, &assignments); err != nil {
		return err
	}
	for name, port := range assignments {
		pa.byName[name] = port
		pa.byPort[port] = name
	}
	return nil
}

func (pa *PortAllocator) save() error {
	data, err := json.Marshal(pa.byName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pa.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(pa.path, data, 0600)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/gandalfledev/Developer/hopbox && go test ./internal/hostd/ -v -run TestPortAllocator`
Expected: all 5 tests PASS

**Step 5: Commit**

```bash
git add internal/hostd/portalloc.go internal/hostd/portalloc_test.go
git commit -m "feat: add port allocator for workspace UDP ports"
```

---

### Task 4: Provisioner — agent injection and entropy seeding

This migrates the inject/entropy logic from silo's poc-managed into hopbox.

**Files:**
- Create: `internal/hostd/provisioner.go`

**Step 1: Write the provisioner with inject + entropy**

```go
// internal/hostd/provisioner.go
//go:build linux

package hostd

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/hopboxdev/silo"
)

// Provisioner handles the full workspace provisioning flow:
// inject hop-agent, seed entropy, exchange WireGuard keys,
// start hop-agent, and set up iptables port forwarding.
type Provisioner struct {
	agentBinaryPath string
	hostIP          string
}

// NewProvisioner creates a provisioner.
// agentBinaryPath is the path to the hop-agent linux binary on the host.
// hostIP is the public IP of this host (used in client config endpoint).
func NewProvisioner(agentBinaryPath, hostIP string) *Provisioner {
	return &Provisioner{
		agentBinaryPath: agentBinaryPath,
		hostIP:          hostIP,
	}
}

// ProvisionResult contains the output of a successful provisioning.
type ProvisionResult struct {
	ClientPrivateKey string // base64 WireGuard private key for client
	ServerPublicKey  string // base64 WireGuard public key from server
}

// Provision runs the full provisioning flow on a running VM:
// inject agent → seed entropy → exchange keys → start agent → port forward.
func (p *Provisioner) Provision(ctx context.Context, vm *silo.VM, hostPort int) (*ProvisionResult, error) {
	if err := p.injectAgent(ctx, vm); err != nil {
		return nil, fmt.Errorf("inject agent: %w", err)
	}

	clientPrivB64, serverPubB64, err := p.exchangeKeys(ctx, vm)
	if err != nil {
		return nil, fmt.Errorf("exchange keys: %w", err)
	}

	if err := p.startAgent(ctx, vm); err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	if err := p.setupPortForward(vm.IP(), hostPort); err != nil {
		return nil, fmt.Errorf("port forward: %w", err)
	}

	return &ProvisionResult{
		ClientPrivateKey: clientPrivB64,
		ServerPublicKey:  serverPubB64,
	}, nil
}

// Deprovision removes iptables port forwarding rules for a workspace.
func (p *Provisioner) Deprovision(vmIP string, hostPort int) {
	cleanupPortForward(vmIP, hostPort)
}

func (p *Provisioner) injectAgent(ctx context.Context, vm *silo.VM) error {
	log.Printf("[provisioner] injecting hop-agent into %s", vm.Name)

	hostTapIP := tapIPFromGuestIP(vm.IP())

	// Temporary HTTP server on host TAP IP to serve the binary.
	mux := http.NewServeMux()
	mux.HandleFunc("/hop-agent", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, p.agentBinaryPath)
	})
	addr := net.JoinHostPort(hostTapIP, "18080")
	srv := &http.Server{Addr: addr, Handler: mux}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	url := fmt.Sprintf("http://%s:18080/hop-agent", hostTapIP)
	result, err := vm.Exec(ctx, fmt.Sprintf(
		"curl -sf -o /usr/local/bin/hop-agent %s && chmod +x /usr/local/bin/hop-agent", url))
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("download failed (exit %d): %s", result.ExitCode, result.Stderr)
	}

	if err := p.seedEntropy(ctx, vm); err != nil {
		return fmt.Errorf("seed entropy: %w", err)
	}

	result, err = vm.Exec(ctx, "/usr/local/bin/hop-agent version")
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	log.Printf("[provisioner] hop-agent installed: %s", strings.TrimSpace(result.Stdout))
	return nil
}

// seedEntropy uses perl's ioctl to call RNDADDENTROPY on the guest's /dev/random.
// Firecracker VMs with kernel 4.14 have insufficient entropy for Go's getrandom().
func (p *Provisioner) seedEntropy(ctx context.Context, vm *silo.VM) error {
	script := `perl -e '
use strict;
open(my $u, "<", "/dev/urandom") or die "open urandom: $!";
my $d; read($u, $d, 256) == 256 or die "read: $!";
close($u);
my $p = pack("i i", 2048, 256) . $d;
open(my $r, ">", "/dev/random") or die "open random: $!";
ioctl($r, 0x40085203, $p) or die "ioctl: $!";
close($r);
print "ok\n";
'`
	result, err := vm.Exec(ctx, script)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("perl failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (p *Provisioner) exchangeKeys(ctx context.Context, vm *silo.VM) (clientPrivB64, serverPubB64 string, err error) {
	log.Printf("[provisioner] exchanging WireGuard keys for %s", vm.Name)

	result, err := vm.Exec(ctx, "mkdir -p /etc/hopbox")
	if err != nil || result.ExitCode != 0 {
		return "", "", fmt.Errorf("mkdir /etc/hopbox: %v (exit %d)", err, result.ExitCode)
	}

	// Phase 1: generate server keys
	result, err = vm.Exec(ctx, "/usr/local/bin/hop-agent setup")
	if err != nil {
		return "", "", fmt.Errorf("setup phase 1: %w", err)
	}
	if result.ExitCode != 0 {
		return "", "", fmt.Errorf("setup phase 1 failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	serverPubB64 = strings.TrimSpace(result.Stdout)

	// Generate client keypair
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate client keys: %w", err)
	}
	clientPrivB64 = base64.StdEncoding.EncodeToString(priv.Bytes())
	clientPubB64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())

	// Phase 2: send client public key
	result, err = vm.Exec(ctx, fmt.Sprintf(
		"/usr/local/bin/hop-agent setup --client-pubkey=%s", clientPubB64))
	if err != nil {
		return "", "", fmt.Errorf("setup phase 2: %w", err)
	}
	if result.ExitCode != 0 {
		return "", "", fmt.Errorf("setup phase 2 failed (exit %d): %s", result.ExitCode, result.Stderr)
	}

	return clientPrivB64, serverPubB64, nil
}

func (p *Provisioner) startAgent(ctx context.Context, vm *silo.VM) error {
	log.Printf("[provisioner] starting hop-agent in %s", vm.Name)

	result, err := vm.Exec(ctx, "nohup /usr/local/bin/hop-agent serve > /var/log/hop-agent.log 2>&1 &")
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("start failed (exit %d): %s", result.ExitCode, result.Stderr)
	}

	// Poll for wg0 interface (up to 10s)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		result, err = vm.Exec(ctx, "ip link show wg0 2>/dev/null && echo WG_UP || echo WG_DOWN")
		if err != nil {
			return fmt.Errorf("check wg0: %w", err)
		}
		if strings.Contains(result.Stdout, "WG_UP") {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	logResult, _ := vm.Exec(ctx, "cat /var/log/hop-agent.log")
	return fmt.Errorf("wg0 did not come up within 10s.\nhop-agent log:\n%s", logResult.Stdout)
}

func (p *Provisioner) setupPortForward(vmIP string, hostPort int) error {
	log.Printf("[provisioner] port forward: host:%d -> %s:51820", hostPort, vmIP)

	// DNAT
	if out, err := exec.Command("iptables", "-t", "nat", "-A", "PREROUTING",
		"-p", "udp", "--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:51820", vmIP)).CombinedOutput(); err != nil {
		return fmt.Errorf("DNAT: %w: %s", err, out)
	}

	// FORWARD (insert at top to avoid Docker/Tailscale interference)
	if out, err := exec.Command("iptables", "-I", "FORWARD", "1",
		"-p", "udp", "-d", vmIP, "--dport", "51820",
		"-j", "ACCEPT").CombinedOutput(); err != nil {
		return fmt.Errorf("FORWARD: %w: %s", err, out)
	}

	// Return traffic FORWARD
	if out, err := exec.Command("iptables", "-I", "FORWARD", "1",
		"-p", "udp", "-s", vmIP, "--sport", "51820",
		"-j", "ACCEPT").CombinedOutput(); err != nil {
		return fmt.Errorf("FORWARD return: %w: %s", err, out)
	}

	return nil
}

func cleanupPortForward(vmIP string, hostPort int) {
	exec.Command("iptables", "-t", "nat", "-D", "PREROUTING",
		"-p", "udp", "--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:51820", vmIP)).Run()
	exec.Command("iptables", "-D", "FORWARD",
		"-p", "udp", "-d", vmIP, "--dport", "51820",
		"-j", "ACCEPT").Run()
	exec.Command("iptables", "-D", "FORWARD",
		"-p", "udp", "-s", vmIP, "--sport", "51820",
		"-j", "ACCEPT").Run()
}

// tapIPFromGuestIP derives the host TAP IP from the guest IP.
// In a /30 subnet: network+1 = host, network+2 = guest.
func tapIPFromGuestIP(guestIP string) string {
	ip := net.ParseIP(guestIP).To4()
	ip[3]--
	return ip.String()
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/gandalfledev/Developer/hopbox && GOOS=linux go build ./internal/hostd/`

Note: This file has `//go:build linux` and imports silo (linux-only). Add silo dependency first:

Run: `cd /Users/gandalfledev/Developer/hopbox && GOOS=linux go get github.com/hopboxdev/silo && GOOS=linux go build ./internal/hostd/`

If silo is not published to a Go module proxy, use a replace directive:

Run: `cd /Users/gandalfledev/Developer/hopbox && go mod edit -replace github.com/hopboxdev/silo=../silo && GOOS=linux go get github.com/hopboxdev/silo && go mod tidy`

Then: `GOOS=linux go build ./internal/hostd/`
Expected: compiles without errors

**Step 3: Commit**

```bash
git add internal/hostd/provisioner.go go.mod go.sum
git commit -m "feat: add provisioner for agent injection and WireGuard setup"
```

---

### Task 5: gRPC server implementation

**Files:**
- Create: `internal/hostd/server.go`

**Step 1: Write the gRPC server**

```go
// internal/hostd/server.go
//go:build linux

package hostd

import (
	"context"
	"fmt"
	"log"
	"runtime"

	"github.com/hopboxdev/silo"
	pb "github.com/hopboxdev/hopbox/gen/hostd/v1"
)

// Server implements the HostService gRPC server.
type Server struct {
	pb.UnimplementedHostServiceServer
	rt          *silo.Runtime
	provisioner *Provisioner
	ports       *PortAllocator
	hostIP      string
	defaults    WorkspaceDefaults
}

// WorkspaceDefaults holds default values for workspace creation.
type WorkspaceDefaults struct {
	Image    string
	VCPUs    int
	MemoryMB int
	DiskGB   int
}

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	Runtime         *silo.Runtime
	Provisioner     *Provisioner
	PortAllocator   *PortAllocator
	HostIP          string
	Defaults        WorkspaceDefaults
}

// NewServer creates a new gRPC server.
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		rt:          cfg.Runtime,
		provisioner: cfg.Provisioner,
		ports:       cfg.PortAllocator,
		hostIP:      cfg.HostIP,
		defaults:    cfg.Defaults,
	}
}

func (s *Server) CreateWorkspace(ctx context.Context, req *pb.CreateWorkspaceRequest) (*pb.CreateWorkspaceResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	image := req.Image
	if image == "" {
		image = s.defaults.Image
	}
	vcpus := int(req.Vcpus)
	if vcpus == 0 {
		vcpus = s.defaults.VCPUs
	}
	memMB := int(req.MemoryMb)
	if memMB == 0 {
		memMB = s.defaults.MemoryMB
	}
	diskGB := int(req.DiskGb)
	if diskGB == 0 {
		diskGB = s.defaults.DiskGB
	}

	log.Printf("[hostd] creating workspace %q", req.Name)

	// Allocate a port first (so we can clean up if VM creation fails).
	hostPort, err := s.ports.Allocate(req.Name)
	if err != nil {
		return nil, fmt.Errorf("allocate port: %w", err)
	}

	// Create and start VM.
	vm, err := s.rt.Create(ctx, silo.VMConfig{
		Name:      req.Name,
		Image:     image,
		VCPUs:     vcpus,
		MemoryMB:  memMB,
		DiskGB:    diskGB,
		Lifecycle: silo.Persistent,
	})
	if err != nil {
		s.ports.Release(req.Name)
		return nil, fmt.Errorf("create VM: %w", err)
	}

	if err := vm.Start(ctx); err != nil {
		vm.Destroy(ctx)
		s.ports.Release(req.Name)
		return nil, fmt.Errorf("start VM: %w", err)
	}

	// Provision: inject agent, exchange keys, start agent, port forward.
	result, err := s.provisioner.Provision(ctx, vm, hostPort)
	if err != nil {
		vm.Destroy(ctx)
		s.ports.Release(req.Name)
		return nil, fmt.Errorf("provision: %w", err)
	}

	log.Printf("[hostd] workspace %q ready on port %d", req.Name, hostPort)

	return &pb.CreateWorkspaceResponse{
		Workspace: workspaceInfo(vm, hostPort),
		ClientConfig: &pb.ClientConfig{
			Name:          req.Name,
			Endpoint:      fmt.Sprintf("%s:%d", s.hostIP, hostPort),
			PrivateKey:    result.ClientPrivateKey,
			PeerPublicKey: result.ServerPublicKey,
			TunnelIp:      "10.10.0.1/24",
			AgentIp:       "10.10.0.2",
		},
	}, nil
}

func (s *Server) DestroyWorkspace(ctx context.Context, req *pb.DestroyWorkspaceRequest) (*pb.DestroyWorkspaceResponse, error) {
	vm, err := s.rt.Get(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	// Clean up iptables rules.
	if port, err := s.ports.Get(req.Name); err == nil {
		s.provisioner.Deprovision(vm.IP(), port)
	}

	if err := vm.Destroy(ctx); err != nil {
		return nil, fmt.Errorf("destroy: %w", err)
	}

	s.ports.Release(req.Name)

	log.Printf("[hostd] workspace %q destroyed", req.Name)
	return &pb.DestroyWorkspaceResponse{}, nil
}

func (s *Server) SuspendWorkspace(ctx context.Context, req *pb.SuspendWorkspaceRequest) (*pb.SuspendWorkspaceResponse, error) {
	vm, err := s.rt.Get(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	// Clean up iptables before suspend (TAP will be released).
	if port, err := s.ports.Get(req.Name); err == nil {
		s.provisioner.Deprovision(vm.IP(), port)
	}

	if err := vm.Suspend(ctx); err != nil {
		return nil, fmt.Errorf("suspend: %w", err)
	}

	log.Printf("[hostd] workspace %q suspended", req.Name)
	return &pb.SuspendWorkspaceResponse{}, nil
}

func (s *Server) ResumeWorkspace(ctx context.Context, req *pb.ResumeWorkspaceRequest) (*pb.ResumeWorkspaceResponse, error) {
	vm, err := s.rt.Get(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	if err := vm.Resume(ctx); err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}

	// Re-provision port forwarding (VM has new TAP IP after resume).
	hostPort, err := s.ports.Get(req.Name)
	if err != nil {
		return nil, fmt.Errorf("get port: %w", err)
	}
	if err := s.provisioner.setupPortForward(vm.IP(), hostPort); err != nil {
		return nil, fmt.Errorf("port forward: %w", err)
	}

	log.Printf("[hostd] workspace %q resumed on port %d", req.Name, hostPort)

	return &pb.ResumeWorkspaceResponse{
		Workspace: workspaceInfo(vm, hostPort),
	}, nil
}

func (s *Server) GetWorkspace(ctx context.Context, req *pb.GetWorkspaceRequest) (*pb.GetWorkspaceResponse, error) {
	vm, err := s.rt.Get(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	port, _ := s.ports.Get(req.Name)

	return &pb.GetWorkspaceResponse{
		Workspace: workspaceInfo(vm, port),
	}, nil
}

func (s *Server) ListWorkspaces(ctx context.Context, _ *pb.ListWorkspacesRequest) (*pb.ListWorkspacesResponse, error) {
	vms, err := s.rt.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}

	infos := make([]*pb.WorkspaceInfo, len(vms))
	for i, vm := range vms {
		port, _ := s.ports.Get(vm.Name)
		infos[i] = workspaceInfo(vm, port)
	}

	return &pb.ListWorkspacesResponse{
		Workspaces: infos,
	}, nil
}

func (s *Server) HostStatus(_ context.Context, _ *pb.HostStatusRequest) (*pb.HostStatusResponse, error) {
	totalCPUs := runtime.NumCPU()

	// TODO: more accurate capacity tracking when needed
	return &pb.HostStatusResponse{
		TotalVcpus:     int32(totalCPUs),
		AvailableVcpus: int32(totalCPUs), // placeholder
	}, nil
}

func workspaceInfo(vm *silo.VM, hostPort int) *pb.WorkspaceInfo {
	return &pb.WorkspaceInfo{
		Name:     vm.Name,
		State:    string(vm.State()),
		Vcpus:    int32(vm.Config.VCPUs),
		MemoryMb: int32(vm.Config.MemoryMB),
		DiskGb:   int32(vm.Config.DiskGB),
		HostPort: int32(hostPort),
		VmIp:     vm.IP(),
	}
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/gandalfledev/Developer/hopbox && GOOS=linux go build ./internal/hostd/`
Expected: compiles without errors

**Step 3: Commit**

```bash
git add internal/hostd/server.go
git commit -m "feat: implement hostd gRPC server with all 7 RPCs"
```

---

### Task 6: Daemon entry point

**Files:**
- Create: `cmd/hopbox-hostd/main.go`

**Step 1: Write the daemon main**

```go
// cmd/hopbox-hostd/main.go
//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"

	"google.golang.org/grpc"

	"github.com/hopboxdev/silo"
	pb "github.com/hopboxdev/hopbox/gen/hostd/v1"
	"github.com/hopboxdev/hopbox/internal/hostd"
)

func main() {
	var (
		listenAddr = flag.String("listen", "127.0.0.1:9090", "gRPC listen address")
		zfsPool    = flag.String("zfs-pool", "silo", "ZFS pool name")
		agentBin   = flag.String("agent-binary", "", "path to hop-agent linux binary (required)")
		hostIP     = flag.String("host-ip", "", "public IP of this host (required)")
		portMin    = flag.Int("port-min", 51820, "minimum UDP port for WireGuard")
		portMax    = flag.Int("port-max", 52820, "maximum UDP port for WireGuard")
		dataDir    = flag.String("data-dir", "/var/lib/hopbox-hostd", "data directory for state files")
	)
	flag.Parse()

	if *agentBin == "" || *hostIP == "" {
		flag.Usage()
		os.Exit(1)
	}

	if _, err := os.Stat(*agentBin); err != nil {
		log.Fatalf("hop-agent binary not found: %s", *agentBin)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, *listenAddr, *zfsPool, *agentBin, *hostIP, *portMin, *portMax, *dataDir); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, listenAddr, zfsPool, agentBin, hostIP string, portMin, portMax int, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	log.Println("Initializing Silo runtime...")
	rt, err := silo.Init(ctx, silo.Options{
		ZFSPool:   zfsPool,
		CIDRRange: "172.16.0.0/16",
	})
	if err != nil {
		return fmt.Errorf("silo init: %w", err)
	}

	ports, err := hostd.NewPortAllocator(portMin, portMax,
		filepath.Join(dataDir, "ports.json"))
	if err != nil {
		return fmt.Errorf("port allocator: %w", err)
	}

	prov := hostd.NewProvisioner(agentBin, hostIP)

	srv := hostd.NewServer(hostd.ServerConfig{
		Runtime:       rt,
		Provisioner:   prov,
		PortAllocator: ports,
		HostIP:        hostIP,
		Defaults: hostd.WorkspaceDefaults{
			Image:    "ubuntu-dev",
			VCPUs:    2,
			MemoryMB: 2048,
			DiskGB:   10,
		},
	})

	grpcServer := grpc.NewServer()
	pb.RegisterHostServiceServer(grpcServer, srv)

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	log.Printf("hopbox-hostd listening on %s", listenAddr)

	// Graceful shutdown on context cancel.
	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")
		grpcServer.GracefulStop()
	}()

	if err := grpcServer.Serve(ln); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/gandalfledev/Developer/hopbox && GOOS=linux go build -o /dev/null ./cmd/hopbox-hostd/`
Expected: compiles without errors

**Step 3: Commit**

```bash
git add cmd/hopbox-hostd/main.go
git commit -m "feat: add hopbox-hostd daemon entry point"
```

---

### Task 7: Add build target to Makefile

**Files:**
- Modify: `Makefile`

**Step 1: Read the current Makefile**

Run: `cat /Users/gandalfledev/Developer/hopbox/Makefile`

**Step 2: Add hopbox-hostd build target**

Add a new target that cross-compiles hopbox-hostd for linux/amd64 alongside the existing hop-agent-linux target. The exact Makefile edits depend on the current structure, but the build command is:

```makefile
dist/hopbox-hostd: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS_FLAG) -o $@ ./cmd/hopbox-hostd/
```

Add `dist/hopbox-hostd` to the `build` target's dependencies.

**Step 3: Verify the build**

Run: `cd /Users/gandalfledev/Developer/hopbox && make dist/hopbox-hostd` (or equivalent)
Expected: produces `dist/hopbox-hostd` binary

**Step 4: Commit**

```bash
git add Makefile
git commit -m "build: add hopbox-hostd build target"
```

---

### Task 8: Add buf generate to Makefile

**Files:**
- Modify: `Makefile`

**Step 1: Add proto generation target**

```makefile
.PHONY: proto
proto:
	buf generate
```

**Step 2: Verify**

Run: `make proto`
Expected: regenerates gen/hostd/v1/*.pb.go

**Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add proto generation target"
```

---

### Task 9: End-to-end test on VPS

**Files:** none (manual test)

**Step 1: Build and upload**

```bash
cd /Users/gandalfledev/Developer/hopbox && make build
scp dist/hopbox-hostd vps:~/hopbox-hostd
scp dist/hop-agent-linux vps:~/hop-agent-linux
```

**Step 2: Run hopbox-hostd on VPS**

```bash
ssh vps
# Clean any leftover state from poc-managed
sudo rm -f /silo-test/.silo/state.db
sudo zfs destroy -r silo-test/vms 2>/dev/null

sudo ./hopbox-hostd \
  -zfs-pool silo-test \
  -agent-binary ~/hop-agent-linux \
  -host-ip 51.38.50.59
```

Expected: `hopbox-hostd listening on 127.0.0.1:9090`

**Step 3: Create a workspace via grpcurl**

In another VPS terminal:

```bash
grpcurl -plaintext -d '{"name":"test-ws"}' \
  localhost:9090 hostd.v1.HostService/CreateWorkspace
```

Expected: JSON response with `workspace` and `client_config` fields.

**Step 4: Verify from laptop**

Save the `client_config` from the grpcurl response as `~/.config/hopbox/hosts/test-ws.yaml` (convert from JSON to YAML format), then:

```bash
hop up -H test-ws
hop status -H test-ws
```

Expected: tunnel connected, agent responsive.

**Step 5: Destroy workspace**

```bash
grpcurl -plaintext -d '{"name":"test-ws"}' \
  localhost:9090 hostd.v1.HostService/DestroyWorkspace
```

Expected: workspace destroyed, VM gone, iptables cleaned up.

**Step 6: Verify cleanup**

```bash
sudo iptables -t nat -L PREROUTING -n | grep 51820  # should be empty
sudo zfs list | grep test-ws                          # should be empty
```

---

### Task 10: Install grpcurl on VPS (if needed)

If grpcurl is not available on the VPS:

```bash
ssh vps
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

Or download a release binary. This is a prerequisite for Task 9.
