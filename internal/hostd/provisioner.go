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
// inject agent -> seed entropy -> exchange keys -> start agent -> port forward.
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
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

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
	_ = exec.Command("iptables", "-t", "nat", "-D", "PREROUTING",
		"-p", "udp", "--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:51820", vmIP)).Run()
	_ = exec.Command("iptables", "-D", "FORWARD",
		"-p", "udp", "-d", vmIP, "--dport", "51820",
		"-j", "ACCEPT").Run()
	_ = exec.Command("iptables", "-D", "FORWARD",
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
