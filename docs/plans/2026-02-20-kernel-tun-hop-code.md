# Kernel TUN + `hop code` Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace netstack with kernel TUN via a privileged helper daemon so `10.10.0.2` is a real system IP, add `hop code` command, remove `hop shell`.

**Architecture:** A LaunchDaemon helper (`dev.hopbox.helper`) handles privileged ops (TUN IP assignment, routing, /etc/hosts). `hop up` creates the utun device itself (unprivileged on macOS), then delegates IP/route config to the helper via Unix socket. All RPC simplifies to plain HTTP against `<name>.hop:4200`.

**Tech Stack:** wireguard-go kernel TUN (`tun.CreateTUN`), LaunchDaemon plist, Unix domain socket, JSON protocol

**Design doc:** `docs/plans/2026-02-20-kernel-tun-hop-code-design.md`

---

## Note: `hop to` and temporary tunnels

`hop to` currently creates a temporary netstack tunnel to the target host for restore. With kernel TUN, both source and target use `10.10.0.2`, creating a routing conflict. For this plan, `hop to` keeps using netstack for its temporary tunnel (it's short-lived and in-process). A follow-up can revisit this if needed. The netstack library stays available — we only remove it from `ClientTunnel` used by `hop up`.

---

### Task 1: Helper protocol types

**Files:**
- Create: `internal/helper/protocol.go`
- Test: `internal/helper/protocol_test.go`

**Step 1: Write the failing test**

```go
// internal/helper/protocol_test.go
package helper

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshal(t *testing.T) {
	req := Request{
		Action:    ActionConfigureTUN,
		Interface: "utun5",
		LocalIP:   "10.10.0.1",
		PeerIP:    "10.10.0.2",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != req {
		t.Errorf("got %+v, want %+v", got, req)
	}
}

func TestResponseMarshalError(t *testing.T) {
	resp := Response{OK: false, Error: "permission denied"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var got Response
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK || got.Error != "permission denied" {
		t.Errorf("got %+v", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/helper/... -run TestRequest -v`
Expected: FAIL — package doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/helper/protocol.go
package helper

const (
	ActionConfigureTUN = "configure_tun"
	ActionCleanupTUN   = "cleanup_tun"
	ActionAddHost      = "add_host"
	ActionRemoveHost   = "remove_host"
)

// Request is sent from hop to the helper daemon over the Unix socket.
type Request struct {
	Action    string `json:"action"`
	Interface string `json:"interface,omitempty"`
	LocalIP   string `json:"local_ip,omitempty"`
	PeerIP    string `json:"peer_ip,omitempty"`
	IP        string `json:"ip,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
}

// Response is sent back from the helper daemon.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SocketPath is where the helper daemon listens.
const SocketPath = "/var/run/hopbox/helper.sock"
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/helper/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/helper/protocol.go internal/helper/protocol_test.go
git commit -m "feat: add helper daemon protocol types"
```

---

### Task 2: Helper /etc/hosts management

**Files:**
- Create: `internal/helper/hosts.go`
- Test: `internal/helper/hosts_test.go`

**Step 1: Write the failing test**

```go
// internal/helper/hosts_test.go
package helper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddHostEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0644)

	if err := addHostEntry(path, "10.10.0.2", "mybox.hop"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestAddHostEntryIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0644)

	addHostEntry(path, "10.10.0.2", "mybox.hop")
	addHostEntry(path, "10.10.0.2", "mybox.hop")

	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestAddMultipleHosts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0644)

	addHostEntry(path, "10.10.0.2", "mybox.hop")
	addHostEntry(path, "10.10.0.3", "gaming.hop")

	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n10.10.0.3 gaming.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestRemoveHostEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	os.WriteFile(path, []byte("127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n# --- hopbox managed end ---\n"), 0644)

	if err := removeHostEntry(path, "mybox.hop"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestRemoveOneOfMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	os.WriteFile(path, []byte("127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.2 mybox.hop\n10.10.0.3 gaming.hop\n# --- hopbox managed end ---\n"), 0644)

	removeHostEntry(path, "mybox.hop")

	data, _ := os.ReadFile(path)
	want := "127.0.0.1 localhost\n# --- hopbox managed start ---\n10.10.0.3 gaming.hop\n# --- hopbox managed end ---\n"
	if string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/helper/... -run TestAddHost -v`
Expected: FAIL — `addHostEntry` undefined

**Step 3: Write minimal implementation**

```go
// internal/helper/hosts.go
package helper

import (
	"fmt"
	"os"
	"strings"
)

const (
	markerStart = "# --- hopbox managed start ---"
	markerEnd   = "# --- hopbox managed end ---"
)

// addHostEntry adds an IP→hostname mapping to the managed section of the
// hosts file. Creates the managed section if it doesn't exist. Idempotent.
func addHostEntry(path, ip, hostname string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)
	entry := ip + " " + hostname

	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)

	if startIdx == -1 || endIdx == -1 {
		// No managed section — append one.
		content = strings.TrimRight(content, "\n") + "\n" +
			markerStart + "\n" + entry + "\n" + markerEnd + "\n"
	} else {
		// Extract existing managed entries.
		sectionStart := startIdx + len(markerStart) + 1
		section := content[sectionStart:endIdx]
		lines := strings.Split(strings.TrimRight(section, "\n"), "\n")

		// Check if entry already exists.
		for _, line := range lines {
			if strings.TrimSpace(line) == entry {
				return nil // already present
			}
		}

		// Remove any existing entry for this hostname (update case).
		var kept []string
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == hostname {
				continue
			}
			if strings.TrimSpace(line) != "" {
				kept = append(kept, line)
			}
		}
		kept = append(kept, entry)

		newSection := strings.Join(kept, "\n")
		content = content[:startIdx] + markerStart + "\n" + newSection + "\n" + markerEnd + "\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}

// removeHostEntry removes a hostname from the managed section. Removes the
// entire managed section if it becomes empty.
func removeHostEntry(path, hostname string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)

	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx == -1 || endIdx == -1 {
		return nil // no managed section
	}

	sectionStart := startIdx + len(markerStart) + 1
	section := content[sectionStart:endIdx]
	lines := strings.Split(strings.TrimRight(section, "\n"), "\n")

	var kept []string
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == hostname {
			continue
		}
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}

	if len(kept) == 0 {
		// Remove entire managed section.
		content = content[:startIdx]
	} else {
		newSection := strings.Join(kept, "\n")
		content = content[:startIdx] + markerStart + "\n" + newSection + "\n" + markerEnd + "\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/helper/... -run TestAddHost -v && go test ./internal/helper/... -run TestRemove -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/helper/hosts.go internal/helper/hosts_test.go
git commit -m "feat: add /etc/hosts managed section read/write"
```

---

### Task 3: Helper TUN configuration (macOS)

**Files:**
- Create: `internal/helper/tun_darwin.go`
- Test: `internal/helper/tun_darwin_test.go`

**Step 1: Write the failing test**

```go
// internal/helper/tun_darwin_test.go
//go:build darwin

package helper

import "testing"

func TestBuildIfconfigArgs(t *testing.T) {
	args := ifconfigArgs("utun5", "10.10.0.1", "10.10.0.2")
	want := []string{"utun5", "inet", "10.10.0.1", "10.10.0.2", "netmask", "255.255.255.0", "up"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildRouteAddArgs(t *testing.T) {
	args := routeAddArgs("utun5")
	want := []string{"-n", "add", "-net", "10.10.0.0/24", "-interface", "utun5"}
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

**Step 2: Run test to verify it fails**

Run: `go test ./internal/helper/... -run TestBuild -v`
Expected: FAIL — functions undefined

**Step 3: Write minimal implementation**

```go
// internal/helper/tun_darwin.go
//go:build darwin

package helper

import (
	"fmt"
	"os/exec"
	"strings"
)

func ifconfigArgs(iface, localIP, peerIP string) []string {
	return []string{iface, "inet", localIP, peerIP, "netmask", "255.255.255.0", "up"}
}

func routeAddArgs(iface string) []string {
	return []string{"-n", "add", "-net", "10.10.0.0/24", "-interface", iface}
}

func routeDelArgs() []string {
	return []string{"-n", "delete", "-net", "10.10.0.0/24"}
}

// configureTUN assigns an IP to the interface and adds a route.
func configureTUN(iface, localIP, peerIP string) error {
	if out, err := exec.Command("ifconfig", ifconfigArgs(iface, localIP, peerIP)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("route", routeAddArgs(iface)...).CombinedOutput(); err != nil {
		return fmt.Errorf("route add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cleanupTUN removes the route. The utun device is destroyed when the
// creating process closes its file descriptor.
func cleanupTUN() error {
	out, err := exec.Command("route", routeDelArgs()...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/helper/... -run TestBuild -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/helper/tun_darwin.go internal/helper/tun_darwin_test.go
git commit -m "feat: add macOS TUN configuration helpers"
```

---

### Task 4: Helper client library

**Files:**
- Create: `internal/helper/client.go`
- Test: `internal/helper/client_test.go`

**Step 1: Write the failing test**

```go
// internal/helper/client_test.go
package helper

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func startMockHelper(t *testing.T, handler func(Request) Response) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				dec := json.NewDecoder(conn)
				enc := json.NewEncoder(conn)
				var req Request
				if dec.Decode(&req) == nil {
					resp := handler(req)
					enc.Encode(resp)
				}
			}()
		}
	}()
	return sock
}

func TestClientConfigureTUN(t *testing.T) {
	var got Request
	sock := startMockHelper(t, func(req Request) Response {
		got = req
		return Response{OK: true}
	})

	c := &Client{SocketPath: sock}
	err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionConfigureTUN || got.Interface != "utun5" {
		t.Errorf("unexpected request: %+v", got)
	}
}

func TestClientAddHost(t *testing.T) {
	var got Request
	sock := startMockHelper(t, func(req Request) Response {
		got = req
		return Response{OK: true}
	})

	c := &Client{SocketPath: sock}
	err := c.AddHost("10.10.0.2", "mybox.hop")
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionAddHost || got.Hostname != "mybox.hop" {
		t.Errorf("unexpected request: %+v", got)
	}
}

func TestClientErrorResponse(t *testing.T) {
	sock := startMockHelper(t, func(req Request) Response {
		return Response{OK: false, Error: "not running as root"}
	})

	c := &Client{SocketPath: sock}
	err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "helper: not running as root" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClientSocketNotFound(t *testing.T) {
	c := &Client{SocketPath: "/nonexistent/helper.sock"}
	err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIsInstalled(t *testing.T) {
	// Non-existent socket → not installed.
	c := &Client{SocketPath: "/nonexistent/helper.sock"}
	if c.IsReachable() {
		t.Error("expected not reachable")
	}

	// Create a mock helper → reachable.
	sock := startMockHelper(t, func(req Request) Response {
		return Response{OK: true}
	})
	c2 := &Client{SocketPath: sock}
	if !c2.IsReachable() {
		t.Error("expected reachable")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/helper/... -run TestClient -v`
Expected: FAIL — `Client` undefined

**Step 3: Write minimal implementation**

```go
// internal/helper/client.go
package helper

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client communicates with the helper daemon over a Unix socket.
type Client struct {
	SocketPath string
}

// NewClient returns a Client using the default socket path.
func NewClient() *Client {
	return &Client{SocketPath: SocketPath}
}

func (c *Client) send(req Request) error {
	conn, err := net.DialTimeout("unix", c.SocketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to helper at %s: %w", c.SocketPath, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("helper: %s", resp.Error)
	}
	return nil
}

// ConfigureTUN asks the helper to assign an IP and add a route for the interface.
func (c *Client) ConfigureTUN(iface, localIP, peerIP string) error {
	return c.send(Request{
		Action:    ActionConfigureTUN,
		Interface: iface,
		LocalIP:   localIP,
		PeerIP:    peerIP,
	})
}

// CleanupTUN asks the helper to remove routes for the tunnel.
func (c *Client) CleanupTUN(iface string) error {
	return c.send(Request{
		Action:    ActionCleanupTUN,
		Interface: iface,
	})
}

// AddHost asks the helper to add an /etc/hosts entry.
func (c *Client) AddHost(ip, hostname string) error {
	return c.send(Request{
		Action:   ActionAddHost,
		IP:       ip,
		Hostname: hostname,
	})
}

// RemoveHost asks the helper to remove an /etc/hosts entry.
func (c *Client) RemoveHost(hostname string) error {
	return c.send(Request{
		Action:   ActionRemoveHost,
		Hostname: hostname,
	})
}

// IsReachable returns true if the helper daemon is responding.
func (c *Client) IsReachable() bool {
	conn, err := net.DialTimeout("unix", c.SocketPath, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/helper/... -run TestClient -v && go test ./internal/helper/... -run TestIsInstalled -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/helper/client.go internal/helper/client_test.go
git commit -m "feat: add helper client library"
```

---

### Task 5: Helper daemon binary

**Files:**
- Create: `cmd/hop-helper/main.go`

**Step 1: Write the daemon**

```go
// cmd/hop-helper/main.go
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
)

func main() {
	log.SetPrefix("hop-helper: ")
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if os.Geteuid() != 0 {
		log.Fatal("must run as root")
	}

	sockDir := filepath.Dir(helper.SocketPath)
	if err := os.MkdirAll(sockDir, 0755); err != nil {
		log.Fatalf("create socket dir: %v", err)
	}
	// Remove stale socket.
	os.Remove(helper.SocketPath)

	ln, err := net.Listen("unix", helper.SocketPath)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Allow non-root users to connect.
	if err := os.Chmod(helper.SocketPath, 0666); err != nil {
		log.Fatalf("chmod socket: %v", err)
	}

	log.Printf("listening on %s", helper.SocketPath)

	// Graceful shutdown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		ln.Close()
		os.Remove(helper.SocketPath)
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer conn.Close()

	var req helper.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeError(conn, fmt.Sprintf("decode request: %v", err))
		return
	}

	log.Printf("action=%s interface=%s hostname=%s", req.Action, req.Interface, req.Hostname)

	var err error
	switch req.Action {
	case helper.ActionConfigureTUN:
		err = configureTUN(req.Interface, req.LocalIP, req.PeerIP)
	case helper.ActionCleanupTUN:
		err = cleanupTUN()
	case helper.ActionAddHost:
		err = addHostEntry("/etc/hosts", req.IP, req.Hostname)
	case helper.ActionRemoveHost:
		err = removeHostEntry("/etc/hosts", req.Hostname)
	default:
		err = fmt.Errorf("unknown action %q", req.Action)
	}

	if err != nil {
		writeError(conn, err.Error())
		return
	}
	json.NewEncoder(conn).Encode(helper.Response{OK: true})
}

func writeError(conn net.Conn, msg string) {
	json.NewEncoder(conn).Encode(helper.Response{OK: false, Error: msg})
}
```

Note: `configureTUN`, `cleanupTUN`, `addHostEntry`, `removeHostEntry` are in `internal/helper/` — the daemon binary imports them. Since `tun_darwin.go` and `hosts.go` are in the `helper` package, the daemon can call them directly. However, they are currently unexported. **Export them** by capitalizing: `ConfigureTUN`, `CleanupTUN`, `AddHostEntry`, `RemoveHostEntry`. Update the daemon to call `helper.ConfigureTUN(...)` etc.

**Step 2: Build and verify**

Run: `go build ./cmd/hop-helper/...`
Expected: builds without errors

**Step 3: Commit**

```bash
git add cmd/hop-helper/main.go
git commit -m "feat: add hop-helper privileged daemon"
```

---

### Task 6: Helper LaunchDaemon installation

**Files:**
- Create: `internal/helper/install_darwin.go`
- Test: `internal/helper/install_darwin_test.go`

**Step 1: Write the failing test**

```go
// internal/helper/install_darwin_test.go
//go:build darwin

package helper

import (
	"strings"
	"testing"
)

func TestPlistContent(t *testing.T) {
	plist := buildPlist("/usr/local/bin/hop-helper")
	if !strings.Contains(plist, "dev.hopbox.helper") {
		t.Error("missing label")
	}
	if !strings.Contains(plist, "/usr/local/bin/hop-helper") {
		t.Error("missing binary path")
	}
	if !strings.Contains(plist, "RunAtLoad") {
		t.Error("missing RunAtLoad")
	}
	if !strings.Contains(plist, "KeepAlive") {
		t.Error("missing KeepAlive")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/helper/... -run TestPlist -v`
Expected: FAIL — `buildPlist` undefined

**Step 3: Write minimal implementation**

```go
// internal/helper/install_darwin.go
//go:build darwin

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	launchDaemonLabel = "dev.hopbox.helper"
	plistPath         = "/Library/LaunchDaemons/dev.hopbox.helper.plist"
	helperInstallPath = "/Library/PrivilegedHelperTools/dev.hopbox.helper"
)

func buildPlist(binaryPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`, launchDaemonLabel, binaryPath)
}

// Install copies the helper binary and installs the LaunchDaemon.
// Must be run with sudo.
func Install(helperBinary string) error {
	// Copy binary.
	if err := os.MkdirAll("/Library/PrivilegedHelperTools", 0755); err != nil {
		return fmt.Errorf("create helper dir: %w", err)
	}
	data, err := os.ReadFile(helperBinary)
	if err != nil {
		return fmt.Errorf("read helper binary: %w", err)
	}
	if err := os.WriteFile(helperInstallPath, data, 0755); err != nil {
		return fmt.Errorf("write helper binary: %w", err)
	}

	// Write plist.
	plist := buildPlist(helperInstallPath)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load the daemon.
	exec.Command("launchctl", "unload", plistPath).Run() // ignore error — may not be loaded
	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsInstalled checks if the helper LaunchDaemon is installed.
func IsInstalled() bool {
	_, err := os.Stat(plistPath)
	return err == nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/helper/... -run TestPlist -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/helper/install_darwin.go internal/helper/install_darwin_test.go
git commit -m "feat: add LaunchDaemon installation for helper"
```

---

### Task 7: Rewrite ClientTunnel for kernel TUN

This is the big one. We create a new `client_darwin.go` that uses `tun.CreateTUN` instead of netstack.

**Files:**
- Create: `internal/tunnel/client_darwin.go`
- Rename: `internal/tunnel/client.go` → `internal/tunnel/client_netstack.go` (keep for `hop to`)
- Test: `internal/tunnel/client_darwin_test.go`

**Step 1: Rename existing client.go**

The existing `client.go` (netstack-based) is still needed by `hop to` for temporary tunnels. Rename it and add a build tag or make it a separate type.

Approach: Keep `ClientTunnel` (netstack) renamed to `NetstackTunnel` for `hop to`. Create `ClientTunnel` (kernel TUN) in `client_darwin.go` for `hop up`.

Actually, simpler: keep `client.go` as-is (it's the netstack tunnel used by `hop to`). Create `KernelTunnel` in `client_darwin.go` for `hop up`. Update `hop up` to use `KernelTunnel` instead of `ClientTunnel`.

```go
// internal/tunnel/client_darwin.go
//go:build darwin

package tunnel

import (
	"context"
	"fmt"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// KernelTunnel is a kernel-mode WireGuard tunnel for macOS.
// It creates a utun device that is visible system-wide — any process can
// connect to the peer IP without DialContext.
type KernelTunnel struct {
	cfg      Config
	dev      *device.Device
	ifName   string
	ready    chan struct{}
	stopOnce sync.Once
}

// NewKernelTunnel creates a new (not yet started) kernel tunnel.
func NewKernelTunnel(cfg Config) *KernelTunnel {
	return &KernelTunnel{cfg: cfg, ready: make(chan struct{})}
}

// Start brings up the kernel TUN device and WireGuard protocol.
// Blocks until ctx is cancelled, then tears down.
func (t *KernelTunnel) Start(ctx context.Context) error {
	tunDev, err := tun.CreateTUN("utun", t.cfg.MTU)
	if err != nil {
		return fmt.Errorf("CreateTUN: %w", err)
	}

	name, err := tunDev.Name()
	if err != nil {
		tunDev.Close()
		return fmt.Errorf("get TUN name: %w", err)
	}
	t.ifName = name

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

// Stop tears down the WireGuard device. The utun interface is destroyed
// automatically when the fd is closed.
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

// InterfaceName returns the utun interface name (e.g. "utun5").
// Only valid after Ready() has closed.
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

**Step 2: Write test**

```go
// internal/tunnel/client_darwin_test.go
//go:build darwin

package tunnel

import "testing"

func TestNewKernelTunnel(t *testing.T) {
	cfg := DefaultClientConfig()
	kt := NewKernelTunnel(cfg)
	if kt == nil {
		t.Fatal("expected non-nil")
	}
	// Ready should not be closed yet.
	select {
	case <-kt.Ready():
		t.Fatal("ready should not be closed before Start")
	default:
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/tunnel/... -run TestNewKernelTunnel -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/tunnel/client_darwin.go internal/tunnel/client_darwin_test.go
git commit -m "feat: add KernelTunnel for macOS using utun"
```

---

### Task 8: Simplify TunnelState

**Files:**
- Modify: `internal/tunnel/state.go`

Remove proxy-specific fields. Add `Hostname` field for `<name>.hop`.

**Step 1: Update TunnelState struct**

In `internal/tunnel/state.go`, change:

```go
// Before (lines 15-24):
type TunnelState struct {
	PID          int               `json:"pid"`
	Host         string            `json:"host"`
	AgentAPIAddr string            `json:"agent_api_addr"`
	SSHAddr      string            `json:"ssh_addr,omitempty"`
	ServicePorts map[string]string `json:"service_ports,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	Connected    bool              `json:"connected"`
	LastHealthy  time.Time         `json:"last_healthy,omitempty"`
}
```

To:

```go
type TunnelState struct {
	PID         int       `json:"pid"`
	Host        string    `json:"host"`
	Hostname    string    `json:"hostname"`               // "<name>.hop"
	Interface   string    `json:"interface,omitempty"`     // "utun5"
	StartedAt   time.Time `json:"started_at"`
	Connected   bool      `json:"connected"`
	LastHealthy time.Time `json:"last_healthy,omitempty"`
}
```

**Step 2: Fix compilation errors**

All code referencing `AgentAPIAddr`, `SSHAddr`, `ServicePorts` must be updated. These are in:
- `cmd/hop/up.go` (writes state) — will be rewritten in Task 9
- `cmd/hop/shell.go` (reads SSHAddr) — will be deleted in Task 11
- `cmd/hop/status.go:59-62` (reads AgentAPIAddr) — simplify to use hostname
- `internal/rpcclient/client.go:64` (reads AgentAPIAddr) — simplify in Task 10

For now, update `status.go` to use hostname:

```go
// status.go lines 59-63, change to:
	agentAddr := fmt.Sprintf("%s.hop:%d", hostName, tunnel.AgentAPIPort)
	agentURL := "http://" + agentAddr + "/health"
```

**Step 3: Run tests**

Run: `go test ./...`
Expected: PASS (after fixing all references)

**Step 4: Commit**

```bash
git add internal/tunnel/state.go cmd/hop/status.go
git commit -m "refactor: simplify TunnelState — remove proxy fields, add Hostname"
```

---

### Task 9: Rewrite `hop up` to use KernelTunnel + helper

**Files:**
- Modify: `cmd/hop/up.go`

This is the largest single change. The new flow:

1. Create KernelTunnel (creates utun device)
2. Start tunnel (WireGuard protocol)
3. Wait for ready
4. Ask helper to configure TUN (IP + route) and add /etc/hosts entry
5. Use plain `http.Client` for all agent communication (no DialContext)
6. Remove all proxy setup
7. Write simplified TunnelState
8. On shutdown: ask helper to remove host entry and clean up TUN

**Step 1: Rewrite up.go**

Key changes to `cmd/hop/up.go`:

Replace `tunnel.NewClientTunnel(tunCfg)` with `tunnel.NewKernelTunnel(tunCfg)`.

After `<-tun.Ready()`, add helper calls:

```go
	// Configure the TUN interface via the privileged helper.
	helperClient := helper.NewClient()
	if !helperClient.IsReachable() {
		return fmt.Errorf("hopbox helper is not running; reinstall with 'hop setup'")
	}

	localIP := strings.TrimSuffix(tunCfg.LocalIP, "/24")
	peerIP := strings.TrimSuffix(tunCfg.PeerIP, "/32")
	if err := helperClient.ConfigureTUN(tun.InterfaceName(), localIP, peerIP); err != nil {
		return fmt.Errorf("configure TUN: %w", err)
	}
	defer helperClient.CleanupTUN(tun.InterfaceName())

	hostname := cfg.Name + ".hop"
	if err := helperClient.AddHost(cfg.AgentIP, hostname); err != nil {
		return fmt.Errorf("add host entry: %w", err)
	}
	defer helperClient.RemoveHost(hostname)
```

Replace the `agentClient` with a plain client:

```go
	agentClient := &http.Client{Timeout: agentClientTimeout}
```

Replace the `agentURL`:

```go
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)
```

Replace `rpcclient.CallVia(agentClient, ...)` with `rpcclient.Call(hostName, ...)`.

Remove ALL proxy setup code (lines 192-247).

Simplify TunnelState:

```go
	state := &tunnel.TunnelState{
		PID:       os.Getpid(),
		Host:      hostName,
		Hostname:  hostname,
		Interface: tun.InterfaceName(),
		StartedAt: time.Now(),
		Connected: true,
		LastHealthy: time.Now(),
	}
```

Update monitor to use plain client:

```go
	monitor := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: agentURL,
		Client:    &http.Client{Timeout: 3 * time.Second},
		...
	})
```

**Step 2: Remove unused imports**

Remove `tunnel.StartProxy`, `tun.DialContext` references. Add `helper` import.

**Step 3: Build and verify**

Run: `go build ./cmd/hop/...`
Expected: builds without errors

**Step 4: Commit**

```bash
git add cmd/hop/up.go
git commit -m "feat: rewrite hop up to use KernelTunnel + helper"
```

---

### Task 10: Simplify RPC client

**Files:**
- Modify: `internal/rpcclient/client.go`

**Step 1: Simplify Call and remove CallVia**

```go
// internal/rpcclient/client.go — new version

package rpcclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hopboxdev/hopbox/internal/tunnel"
)

func doRPC(client *http.Client, url, method string, params any) (json.RawMessage, error) {
	// unchanged
}

// Call makes an RPC call using the host's .hop hostname.
func Call(hostName, method string, params any) (json.RawMessage, error) {
	if hostName == "" {
		return nil, fmt.Errorf("--host <name> required")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://%s.hop:%d/rpc", hostName, tunnel.AgentAPIPort)
	return doRPC(client, url, method, params)
}

// CallAndPrint calls Call and prints the JSON result to stdout.
func CallAndPrint(hostName, method string, params any) error {
	// unchanged
}

// CopyTo sends an RPC request and copies the plain-text response body to dst.
func CopyTo(hostName, method string, params any, dst io.Writer) error {
	if hostName == "" {
		return fmt.Errorf("--host <name> required")
	}
	reqBody, _ := json.Marshal(map[string]any{"method": method, "params": params})
	url := fmt.Sprintf("http://%s.hop:%d/rpc", hostName, tunnel.AgentAPIPort)

	client := &http.Client{}
	resp, err := client.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("RPC call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var rpcResp struct{ Error string `json:"error"` }
		if json.Unmarshal(body, &rpcResp) == nil && rpcResp.Error != "" {
			return fmt.Errorf("RPC error: %s", rpcResp.Error)
		}
		return fmt.Errorf("RPC error: HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}
```

Remove: `CallVia`, `hostconfig` import, `tunnel.LoadState` usage.

**Step 2: Fix `hop to` compilation**

`cmd/hop/to.go` uses `rpcclient.CallVia`. Since `hop to` still uses netstack for its temporary tunnel, it needs a way to call the target agent through netstack. Add a simple `CallWithClient` function:

```go
// CallWithClient makes an RPC call using the provided HTTP client and host config.
func CallWithClient(client *http.Client, agentIP, method string, params any) (json.RawMessage, error) {
	url := fmt.Sprintf("http://%s:%d/rpc", agentIP, tunnel.AgentAPIPort)
	return doRPC(client, url, method, params)
}
```

Update `to.go` to use `rpcclient.CallWithClient(agentClient, targetCfg.AgentIP, "snap.restore", ...)`.

**Step 3: Build and run tests**

Run: `go build ./... && go test ./internal/rpcclient/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/rpcclient/client.go cmd/hop/to.go
git commit -m "refactor: simplify RPC client — remove CallVia, use <name>.hop"
```

---

### Task 11: Remove `hop shell` + add `hop code`

**Files:**
- Delete: `cmd/hop/shell.go`
- Create: `cmd/hop/code.go`
- Modify: `cmd/hop/main.go`

**Step 1: Delete shell.go and remove from CLI struct**

In `cmd/hop/main.go`, remove line 27:
```go
Shell     ShellCmd    `cmd:"" help:"Drop into remote shell."`
```

Delete `cmd/hop/shell.go`.

**Step 2: Create code.go**

```go
// cmd/hop/code.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// CodeCmd opens VS Code connected to the workspace on the VPS.
type CodeCmd struct {
	Path string `arg:"" optional:"" help:"Remote workspace path (overrides editor.path in hopbox.yaml)."`
}

func (c *CodeCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	state, _ := tunnel.LoadState(hostName)
	if state == nil {
		return fmt.Errorf("tunnel to %q is not running; start it with 'hop up'", hostName)
	}

	hostname := state.Hostname
	if hostname == "" {
		hostname = hostName + ".hop"
	}

	user := cfg.SSHUser
	if user == "" {
		user = "root"
	}

	// Write managed SSH config entry.
	if err := writeSSHConfig(hostname, user, cfg.SSHKeyPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update SSH config: %v\n", err)
	}

	// Resolve workspace path.
	wsPath := c.Path
	if wsPath == "" {
		if ws, err := manifest.Parse("hopbox.yaml"); err == nil && ws.Editor != nil {
			wsPath = ws.Editor.Path
		}
	}
	if wsPath == "" {
		wsPath = "/root"
		if user != "root" {
			wsPath = "/home/" + user
		}
	}

	// Launch VS Code.
	remote := fmt.Sprintf("ssh-remote+%s", hostname)
	fmt.Printf("Opening VS Code: %s:%s\n", hostname, wsPath)
	cmd := exec.Command("code", "--remote", remote, wsPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

const (
	sshMarkerStart = "# --- hopbox managed start ---"
	sshMarkerEnd   = "# --- hopbox managed end ---"
)

func writeSSHConfig(hostname, user, keyPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}
	configPath := filepath.Join(sshDir, "config")

	var content string
	if data, err := os.ReadFile(configPath); err == nil {
		content = string(data)
	}

	// Build the entry.
	var entry strings.Builder
	fmt.Fprintf(&entry, "Host %s\n", hostname)
	fmt.Fprintf(&entry, "  HostName %s\n", hostname)
	fmt.Fprintf(&entry, "  User %s\n", user)
	if keyPath != "" {
		fmt.Fprintf(&entry, "  IdentityFile %s\n", keyPath)
	}

	// Remove existing managed section.
	startIdx := strings.Index(content, sshMarkerStart)
	endIdx := strings.Index(content, sshMarkerEnd)
	if startIdx != -1 && endIdx != -1 {
		// Extract entries for OTHER hosts, keep them.
		sectionStart := startIdx + len(sshMarkerStart) + 1
		section := content[sectionStart:endIdx]
		// Split by "Host " to find individual entries.
		var kept []string
		for _, block := range splitHostBlocks(section) {
			blockHostname := extractHostname(block)
			if blockHostname != "" && blockHostname != hostname {
				kept = append(kept, block)
			}
		}
		kept = append(kept, entry.String())
		newSection := strings.Join(kept, "")
		content = content[:startIdx] + sshMarkerStart + "\n" + newSection + sshMarkerEnd + "\n"
	} else {
		// Append new managed section.
		content = strings.TrimRight(content, "\n") + "\n\n" +
			sshMarkerStart + "\n" + entry.String() + sshMarkerEnd + "\n"
	}

	return os.WriteFile(configPath, []byte(content), 0600)
}

func splitHostBlocks(section string) []string {
	var blocks []string
	lines := strings.Split(section, "\n")
	var current []string
	for _, line := range lines {
		if strings.HasPrefix(line, "Host ") && len(current) > 0 {
			blocks = append(blocks, strings.Join(current, "\n")+"\n")
			current = nil
		}
		if strings.TrimSpace(line) != "" {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n")+"\n")
	}
	return blocks
}

func extractHostname(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "Host ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Host "))
		}
	}
	return ""
}
```

**Step 3: Update manifest Editor type**

In `internal/manifest/manifest.go`, change:

```go
// Before:
Editor   string             `yaml:"editor,omitempty"`

// After:
Editor   *EditorConfig      `yaml:"editor,omitempty"`
```

Add:

```go
// EditorConfig configures the remote editor.
type EditorConfig struct {
	Type       string   `yaml:"type"`                 // "vscode-remote"
	Path       string   `yaml:"path,omitempty"`       // remote workspace path
	Extensions []string `yaml:"extensions,omitempty"` // VS Code extension IDs
}
```

**Step 4: Add CodeCmd to CLI struct**

In `cmd/hop/main.go`, add:

```go
Code      CodeCmd     `cmd:"" help:"Open VS Code connected to the workspace."`
```

Remove:

```go
Shell     ShellCmd    `cmd:"" help:"Drop into remote shell."`
```

**Step 5: Build and verify**

Run: `go build ./cmd/hop/...`
Expected: builds without errors

**Step 6: Commit**

```bash
git rm cmd/hop/shell.go
git add cmd/hop/code.go cmd/hop/main.go internal/manifest/manifest.go
git commit -m "feat: add hop code command; remove hop shell"
```

---

### Task 12: Update `hop setup` to install helper

**Files:**
- Modify: `cmd/hop/setup.go`
- Modify: `internal/setup/bootstrap.go` (add helper build path detection)

**Step 1: Add helper installation to setup.go**

After `setup.Bootstrap(ctx, opts, os.Stdout)` and auto-set default host, add:

```go
	// Install privileged helper if not already present.
	if !helper.IsInstalled() {
		fmt.Println("\nHopbox needs to install a system helper for tunnel networking.")
		fmt.Print("This requires sudo. Proceed? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			helperBin, err := findHelperBinary()
			if err != nil {
				return fmt.Errorf("find helper binary: %w", err)
			}
			// Run the install via sudo.
			cmd := exec.Command("sudo", helperBin, "--install")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("install helper: %w", err)
			}
			fmt.Println("Helper installed.")
		} else {
			fmt.Println("Skipped helper installation. hop up will not work without it.")
		}
	}
```

Where `findHelperBinary()` looks for `hop-helper` next to the `hop` binary or in `$PATH`.

Alternative simpler approach: add a `--install` flag to `hop-helper` that calls `helper.Install(os.Args[0])`. Then `hop setup` runs `sudo hop-helper --install`.

**Step 2: Build**

Run: `go build ./cmd/hop/...`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/hop/setup.go
git commit -m "feat: install helper daemon during hop setup"
```

---

### Task 13: Cleanup — remove dead code

**Files:**
- Delete: `internal/tunnel/proxy.go` (no longer used after `hop up` rewrite)
- Modify: `internal/tunnel/monitor.go:29` — update comment (no longer needs tun.DialContext)
- Modify: `CLAUDE.md` — update architecture docs

**Step 1: Remove proxy.go**

Verify nothing imports it:

Run: `grep -r "tunnel\.StartProxy\|tunnel\.ProxyConfig\|tunnel\.Proxy" cmd/ internal/ --include='*.go'`

If only `up.go` (already rewritten) references it, delete `proxy.go`.

**Step 2: Update CLAUDE.md**

Key changes:
- Remove "Client Wireguard mode: Netstack" → "Client Wireguard mode: Kernel TUN (utun on macOS)"
- Remove DialContext documentation
- Remove proxy documentation
- Document helper daemon
- Add `hop code` to command list
- Remove `hop shell` from command list
- Document `<name>.hop` hostname convention

**Step 3: Commit**

```bash
git rm internal/tunnel/proxy.go
git add internal/tunnel/monitor.go CLAUDE.md
git commit -m "refactor: remove proxy code and update docs for kernel TUN"
```

---

### Task 14: Integration test

**Files:**
- Create: `internal/helper/integration_test.go`

Write an integration test that verifies the full helper client→daemon→response flow using a mock (no root needed). The real kernel TUN test requires root and is manual:

```bash
# Manual test — requires hop setup + sudo for helper installation:
hop setup mybox -a <ip> -u root -k ~/.ssh/key
hop up          # should print "utun5" interface name, not "proxy" addresses
hop status      # should work via mybox.hop
hop code        # should open VS Code
ssh mybox.hop   # should connect directly
```

**Step 1: Commit test**

```bash
git add internal/helper/integration_test.go
git commit -m "test: add helper integration test"
```

---

## Summary of Changes

| What | Action |
|------|--------|
| `internal/helper/` | **New** — protocol, client, hosts, TUN config, install |
| `cmd/hop-helper/` | **New** — privileged helper daemon binary |
| `internal/tunnel/client_darwin.go` | **New** — `KernelTunnel` using utun |
| `cmd/hop/code.go` | **New** — `hop code` command |
| `cmd/hop/up.go` | **Rewrite** — use KernelTunnel + helper, remove proxies |
| `internal/rpcclient/client.go` | **Simplify** — remove CallVia, use `<name>.hop` |
| `internal/tunnel/state.go` | **Simplify** — remove proxy fields, add Hostname |
| `internal/manifest/manifest.go` | **Modify** — Editor string → *EditorConfig |
| `cmd/hop/main.go` | **Modify** — add Code, remove Shell |
| `cmd/hop/status.go` | **Modify** — use `<name>.hop` for health check |
| `cmd/hop/to.go` | **Modify** — use CallWithClient instead of CallVia |
| `cmd/hop/shell.go` | **Delete** |
| `internal/tunnel/proxy.go` | **Delete** |
| `CLAUDE.md` | **Update** — document new architecture |
