# Linux Client Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable `hop up` (kernel TUN tunnel) on Linux client machines by creating Linux-specific helper installation, TUN creation, and client tunnel files.

**Architecture:** Mirror the macOS architecture with Linux equivalents. The helper daemon installs as a systemd service (vs macOS LaunchDaemon), creates TUN devices via wireguard-go's `tun.CreateTUN` (vs macOS AF_SYSTEM utun), and configures IP/routes via the `ip` command (vs macOS `ifconfig`/`route`). The helper protocol, socket path, `/etc/hosts` management, and client code are platform-agnostic and unchanged.

**Tech Stack:** Go, wireguard-go `tun.CreateTUN`, `ip` command, systemd, SCM_RIGHTS fd passing.

---

### Task 1: Create `tun_linux.go` — Linux TUN creation and IP/route configuration

**Files:**
- Create: `internal/helper/tun_linux.go`
- Create: `internal/helper/tun_linux_test.go`

**Step 1: Write `tun_linux_test.go`**

Tests for the argument-building helpers (same pattern as `tun_darwin_test.go`). These don't need root and can run on any OS.

```go
//go:build linux

package helper

import "testing"

func TestIPAddrAddArgs(t *testing.T) {
	args := ipAddrAddArgs("hopbox0", "10.10.0.1/24")
	want := []string{"addr", "add", "10.10.0.1/24", "dev", "hopbox0"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
}

func TestIPLinkUpArgs(t *testing.T) {
	args := ipLinkUpArgs("hopbox0")
	want := []string{"link", "set", "hopbox0", "up"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
}

func TestIPRouteAddArgs(t *testing.T) {
	args := ipRouteAddArgs("hopbox0")
	want := []string{"route", "add", "10.10.0.0/24", "dev", "hopbox0"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
}

func TestIPRouteDelArgs(t *testing.T) {
	args := ipRouteDelArgs("hopbox0")
	want := []string{"route", "del", "10.10.0.0/24", "dev", "hopbox0"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
}

func TestIPLinkDelArgs(t *testing.T) {
	args := ipLinkDelArgs("hopbox0")
	want := []string{"link", "delete", "hopbox0"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
}
```

**Step 2: Write `tun_linux.go`**

```go
//go:build linux

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.zx2c4.com/wireguard/tun"
)

// CreateTUNDevice creates a Linux kernel TUN device using wireguard-go.
// Returns the TUN fd as an *os.File and the interface name.
// Requires CAP_NET_ADMIN. The caller is responsible for closing the file.
func CreateTUNDevice(mtu int) (*os.File, string, error) {
	tunDev, err := tun.CreateTUN("hopbox%d", mtu)
	if err != nil {
		return nil, "", fmt.Errorf("create TUN: %w", err)
	}

	ifName, err := tunDev.Name()
	if err != nil {
		_ = tunDev.Close()
		return nil, "", fmt.Errorf("get TUN name: %w", err)
	}

	// Extract the underlying fd. The wireguard-go NativeTun exposes File().
	nativeTun, ok := tunDev.(*tun.NativeTun)
	if !ok {
		_ = tunDev.Close()
		return nil, "", fmt.Errorf("unexpected TUN type %T", tunDev)
	}
	fd, err := nativeTun.File()
	if err != nil {
		_ = tunDev.Close()
		return nil, "", fmt.Errorf("get TUN fd: %w", err)
	}

	return fd, ifName, nil
}

// SetMTU sets the MTU on a TUN interface via the ip command.
func SetMTU(ifName string, mtu int) error {
	if mtu <= 0 {
		return nil
	}
	out, err := exec.Command("ip", "link", "set", ifName, "mtu", fmt.Sprintf("%d", mtu)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set MTU on %s: %w: %s", ifName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ipAddrAddArgs(iface, localCIDR string) []string {
	return []string{"addr", "add", localCIDR, "dev", iface}
}

func ipLinkUpArgs(iface string) []string {
	return []string{"link", "set", iface, "up"}
}

func ipRouteAddArgs(iface string) []string {
	return []string{"route", "add", "10.10.0.0/24", "dev", iface}
}

func ipRouteDelArgs(iface string) []string {
	return []string{"route", "del", "10.10.0.0/24", "dev", iface}
}

func ipLinkDelArgs(iface string) []string {
	return []string{"link", "delete", iface}
}

// ConfigureTUN assigns an IP to the interface and adds a route.
func ConfigureTUN(iface, localIP, peerIP string) error {
	localCIDR := localIP + "/24"
	if out, err := exec.Command("ip", ipAddrAddArgs(iface, localCIDR)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ip", ipLinkUpArgs(iface)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set up: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ip", ipRouteAddArgs(iface)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip route add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CleanupTUN removes the route and deletes the TUN interface.
// On Linux, TUN devices persist unless explicitly deleted (unlike macOS utun
// which is destroyed when the creating process closes its fd).
func CleanupTUN(iface string) error {
	// Remove route first, then interface.
	_ = exec.Command("ip", ipRouteDelArgs(iface)...).Run()
	out, err := exec.Command("ip", ipLinkDelArgs(iface)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip link delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 3: Verify it compiles**

Run: `GOOS=linux go build ./internal/helper/...`
Expected: compiles (test can't run on macOS, but compilation check passes)

**Step 4: Commit**

```bash
git add internal/helper/tun_linux.go internal/helper/tun_linux_test.go
git commit -m "feat: add Linux TUN creation and IP/route configuration"
```

---

### Task 2: Create `install_linux.go` — systemd helper installation

**Files:**
- Create: `internal/helper/install_linux.go`
- Create: `internal/helper/install_linux_test.go`

**Step 1: Write `install_linux_test.go`**

```go
//go:build linux

package helper

import (
	"strings"
	"testing"
)

func TestSystemdUnitContent(t *testing.T) {
	unit := buildSystemdUnit("/usr/local/bin/hop-helper")
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/hop-helper") {
		t.Error("missing ExecStart with binary path")
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Error("missing Restart=always")
	}
	if !strings.Contains(unit, "[Install]") {
		t.Error("missing [Install] section")
	}
	if !strings.Contains(unit, "WantedBy=multi-user.target") {
		t.Error("missing WantedBy")
	}
}
```

**Step 2: Write `install_linux.go`**

```go
//go:build linux

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	serviceName       = "hopbox-helper"
	unitPath          = "/etc/systemd/system/hopbox-helper.service"
	helperInstallPath = "/usr/local/bin/hop-helper"
)

func buildSystemdUnit(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Hopbox privileged helper daemon
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=always

[Install]
WantedBy=multi-user.target
`, binaryPath)
}

// Install copies the helper binary and installs the systemd service.
// Must be run with sudo.
func Install(helperBinary string) error {
	data, err := os.ReadFile(helperBinary)
	if err != nil {
		return fmt.Errorf("read helper binary: %w", err)
	}
	if err := os.WriteFile(helperInstallPath, data, 0755); err != nil {
		return fmt.Errorf("write helper binary: %w", err)
	}

	unit := buildSystemdUnit(helperInstallPath)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "enable", "--now", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsInstalled checks if the helper systemd service is installed.
func IsInstalled() bool {
	_, err := os.Stat(unitPath)
	return err == nil
}
```

**Step 3: Verify it compiles**

Run: `GOOS=linux go build ./internal/helper/...`
Expected: compiles

**Step 4: Commit**

```bash
git add internal/helper/install_linux.go internal/helper/install_linux_test.go
git commit -m "feat: add Linux systemd helper installation"
```

---

### Task 3: Create `client_linux.go` — Linux kernel tunnel

**Files:**
- Create: `internal/tunnel/client_linux.go`
- Create: `internal/tunnel/client_linux_test.go`

**Step 1: Write `client_linux_test.go`**

Mirror of `client_darwin_test.go` — tests the constructor, not Start.

```go
//go:build linux

package tunnel

import (
	"os"
	"testing"
)

func TestNewKernelTunnel(t *testing.T) {
	cfg := DefaultClientConfig()
	f, err := os.CreateTemp("", "tun-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	kt := NewKernelTunnel(cfg, f, "hopbox0")
	if kt == nil {
		t.Fatal("expected non-nil")
	}
	if kt.InterfaceName() != "hopbox0" {
		t.Fatalf("expected hopbox0, got %s", kt.InterfaceName())
	}
	select {
	case <-kt.Ready():
		t.Fatal("ready should not be closed before Start")
	default:
	}
}
```

**Step 2: Write `client_linux.go`**

Nearly identical to `client_darwin.go` with the linux build tag.

```go
//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"os"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// KernelTunnel is a kernel-mode WireGuard tunnel for Linux.
// It uses a TUN device (created by the privileged helper) that is visible
// system-wide — any process can connect to the peer IP without DialContext.
type KernelTunnel struct {
	cfg      Config
	tunFile  *os.File
	dev      *device.Device
	ifName   string
	ready    chan struct{}
	stopOnce sync.Once
}

// NewKernelTunnel creates a new (not yet started) kernel tunnel.
// tunFile is the pre-opened TUN fd received from the helper daemon.
// ifName is the interface name (e.g. "hopbox0").
func NewKernelTunnel(cfg Config, tunFile *os.File, ifName string) *KernelTunnel {
	return &KernelTunnel{cfg: cfg, tunFile: tunFile, ifName: ifName, ready: make(chan struct{})}
}

// Start brings up the WireGuard protocol on the pre-opened TUN device.
// Blocks until ctx is cancelled, then tears down.
func (t *KernelTunnel) Start(ctx context.Context) error {
	// MTU=0 tells CreateTUNFromFile to skip setMTU — the helper already set it.
	tunDev, err := tun.CreateTUNFromFile(t.tunFile, 0)
	if err != nil {
		return fmt.Errorf("CreateTUNFromFile: %w", err)
	}

	logger := device.NewLogger(device.LogLevelSilent, "")
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)

	ipcConf := BuildClientIPC(t.cfg)
	if err := dev.IpcSet(ipcConf); err != nil {
		dev.Close()
		return fmt.Errorf("IpcSet: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		return fmt.Errorf("device Up: %w", err)
	}

	t.dev = dev
	close(t.ready)

	<-ctx.Done()
	t.Stop()
	return nil
}

// Stop tears down the WireGuard device.
func (t *KernelTunnel) Stop() {
	t.stopOnce.Do(func() {
		if t.dev != nil {
			t.dev.Close()
			t.dev = nil
		}
	})
}

// Ready returns a channel that closes once the TUN device is up.
func (t *KernelTunnel) Ready() <-chan struct{} {
	return t.ready
}

// InterfaceName returns the TUN interface name (e.g. "hopbox0").
func (t *KernelTunnel) InterfaceName() string {
	return t.ifName
}

// Status returns current tunnel metrics.
func (t *KernelTunnel) Status() *Status {
	s := &Status{LocalIP: t.cfg.LocalIP, PeerIP: t.cfg.PeerIP}
	if t.dev == nil {
		return s
	}
	raw, err := t.dev.IpcGet()
	if err != nil {
		return s
	}
	parseIpcOutput(raw, s)
	return s
}
```

**Step 3: Verify it compiles**

Run: `GOOS=linux go build ./internal/tunnel/...`
Expected: compiles

**Step 4: Commit**

```bash
git add internal/tunnel/client_linux.go internal/tunnel/client_linux_test.go
git commit -m "feat: add Linux kernel tunnel (KernelTunnel)"
```

---

### Task 4: Extract `handleCreateTUN` into OS-specific files

**Files:**
- Create: `cmd/hop-helper/handlecreatetun_darwin.go`
- Create: `cmd/hop-helper/handlecreatetun_linux.go`
- Modify: `cmd/hop-helper/main.go`

**Step 1: Create `handlecreatetun_darwin.go`**

Move the existing `handleCreateTUN` function from `main.go` into this darwin-tagged file.

```go
//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/hopboxdev/hopbox/internal/helper"
	"golang.org/x/sys/unix"
)

func handleCreateTUN(conn net.Conn, mtu int) {
	tunFile, err := helper.CreateTUNSocket()
	if err != nil {
		writeError(conn, fmt.Sprintf("create utun: %v", err))
		return
	}
	defer func() { _ = tunFile.Close() }()

	// Discover the interface name from the fd.
	ifName, err := unix.GetsockoptString(int(tunFile.Fd()), 2, 2) // SYSPROTO_CONTROL, UTUN_OPT_IFNAME
	if err != nil {
		writeError(conn, fmt.Sprintf("get utun name: %v", err))
		return
	}

	// Set MTU while we still have root privileges.
	if err := helper.SetMTU(ifName, mtu); err != nil {
		writeError(conn, fmt.Sprintf("set MTU: %v", err))
		return
	}

	resp := helper.Response{OK: true, Interface: ifName}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		writeError(conn, fmt.Sprintf("marshal response: %v", err))
		return
	}

	// Send the JSON response + the utun fd via SCM_RIGHTS.
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		writeError(conn, "internal: expected UnixConn")
		return
	}

	rights := unix.UnixRights(int(tunFile.Fd()))
	if _, _, err := unixConn.WriteMsgUnix(respBytes, rights, nil); err != nil {
		log.Printf("failed to send utun fd: %v", err)
	}
}
```

**Step 2: Create `handlecreatetun_linux.go`**

```go
//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/hopboxdev/hopbox/internal/helper"
	"golang.org/x/sys/unix"
)

func handleCreateTUN(conn net.Conn, mtu int) {
	tunFile, ifName, err := helper.CreateTUNDevice(mtu)
	if err != nil {
		writeError(conn, fmt.Sprintf("create TUN: %v", err))
		return
	}
	defer func() { _ = tunFile.Close() }()

	resp := helper.Response{OK: true, Interface: ifName}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		writeError(conn, fmt.Sprintf("marshal response: %v", err))
		return
	}

	// Send the JSON response + the TUN fd via SCM_RIGHTS.
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		writeError(conn, "internal: expected UnixConn")
		return
	}

	rights := unix.UnixRights(int(tunFile.Fd()))
	if _, _, err := unixConn.WriteMsgUnix(respBytes, rights, nil); err != nil {
		log.Printf("failed to send TUN fd: %v", err)
	}
}
```

**Step 3: Modify `cmd/hop-helper/main.go`**

Remove the `handleCreateTUN` function and the `"golang.org/x/sys/unix"` import (now only in the OS-specific files). Keep everything else unchanged.

The `main.go` should look like:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/version"
)

func main() {
	// ... (unchanged — same as current main.go lines 18-76)
}

func handle(conn net.Conn) {
	// ... (unchanged — same as current main.go lines 78-119)
}

func writeError(conn net.Conn, msg string) {
	_ = json.NewEncoder(conn).Encode(helper.Response{OK: false, Error: msg})
}
```

The `handle` function still calls `handleCreateTUN(conn, req.MTU)` — the compiler resolves it from the OS-specific file.

**Step 4: Verify it compiles on both targets**

Run: `go build ./cmd/hop-helper/... && GOOS=linux go build ./cmd/hop-helper/...`
Expected: compiles for both darwin and linux

**Step 5: Run full tests**

Run: `go test ./...`
Expected: all tests pass (existing darwin tests still work)

**Step 6: Commit**

```bash
git add cmd/hop-helper/handlecreatetun_darwin.go cmd/hop-helper/handlecreatetun_linux.go cmd/hop-helper/main.go
git commit -m "refactor: extract handleCreateTUN into OS-specific files"
```

---

### Task 5: Verify full cross-compilation and update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Cross-compile all binaries for Linux**

Run: `CGO_ENABLED=0 GOOS=linux go build ./cmd/hop/... && CGO_ENABLED=0 GOOS=linux go build ./cmd/hop-helper/... && CGO_ENABLED=0 GOOS=linux go build ./cmd/hop-agent/...`
Expected: all three binaries compile for Linux

**Step 2: Run full test suite**

Run: `go test ./... 2>&1 | tail -25`
Expected: all packages pass

**Step 3: Update ROADMAP.md**

Change: `- [ ] Linux client support — helper daemon or direct TUN setup for Linux laptops`
To: `- [x] Linux client support — helper daemon with systemd, kernel TUN via wireguard-go`

**Step 4: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark Linux client support as complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `internal/helper/tun_linux.go` | Create — `CreateTUNDevice`, `SetMTU`, `ConfigureTUN`, `CleanupTUN`, `ip` arg helpers |
| `internal/helper/tun_linux_test.go` | Create — tests for `ip` argument builders |
| `internal/helper/install_linux.go` | Create — systemd unit install, `IsInstalled` |
| `internal/helper/install_linux_test.go` | Create — test for unit file content |
| `internal/tunnel/client_linux.go` | Create — `KernelTunnel` struct (mirrors darwin) |
| `internal/tunnel/client_linux_test.go` | Create — constructor test (mirrors darwin) |
| `cmd/hop-helper/handlecreatetun_darwin.go` | Create — extracted from main.go |
| `cmd/hop-helper/handlecreatetun_linux.go` | Create — Linux `handleCreateTUN` |
| `cmd/hop-helper/main.go` | Modify — remove `handleCreateTUN` + `unix` import |
| `ROADMAP.md` | Modify — check off Linux client support |
