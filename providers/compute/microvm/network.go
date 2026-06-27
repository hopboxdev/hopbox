//go:build firecracker

package microvm

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/coreos/go-iptables/iptables"
)

// vmNet owns the host-side VM network: one bridge (the gateway the host's hub +
// metadata API listen behind), per-VM tap devices, and IP allocation in the /24.
// Egress NAT lets VMs reach the internet; the security fence (mirroring the
// docker provider) is added when boxd wires this in (F1.5).
type vmNet struct {
	bridge string
	mu     sync.Mutex
	used   map[int]bool // host octet -> allocated
}

func newVMNet(allowHostPorts []string) (*vmNet, error) {
	n := &vmNet{bridge: vmBridge, used: map[int]bool{}}
	if err := n.ensureBridge(); err != nil {
		return nil, err
	}
	n.ensureFence(allowHostPorts) // best-effort egress fence on the VM subnet
	n.reserveExistingTaps()       // survive a restart: don't reuse orphaned VMs' IPs
	return n, nil
}

// reserveExistingTaps marks the octets of any fctap devices already on the host
// as used, so a freshly-started boxd doesn't hand a live (orphaned) VM's IP/tap
// to a new box.
func (n *vmNet) reserveExistingTaps() {
	out, err := exec.Command("ip", "-br", "link", "show").Output()
	if err != nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, o := range tapOctets(string(out)) {
		n.used[o] = true
	}
}

// ensureBridge creates the bridge + gateway IP + egress NAT, idempotently.
func (n *vmNet) ensureBridge() error {
	if err := exec.Command("ip", "link", "show", n.bridge).Run(); err != nil {
		if out, err := exec.Command("ip", "link", "add", n.bridge, "type", "bridge").CombinedOutput(); err != nil {
			return fmt.Errorf("microvm: create bridge %s: %v: %s", n.bridge, err, out)
		}
	}
	_ = exec.Command("ip", "addr", "add", vmBridgeCIDR, "dev", n.bridge).Run() // ignore "exists"
	if out, err := exec.Command("ip", "link", "set", n.bridge, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("microvm: bridge up: %v: %s", err, out)
	}
	_ = os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644)

	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("microvm: iptables: %w", err)
	}
	for _, r := range [][]string{
		{"nat", "POSTROUTING", "-s", vmSubnet, "!", "-o", n.bridge, "-j", "MASQUERADE"},
		// VM -> beyond is governed by the egress fence (ensureFence); here we only
		// permit return traffic back to the VMs.
		{"filter", "FORWARD", "-o", n.bridge, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
	} {
		if err := ipt.AppendUnique(r[0], r[1], r[2:]...); err != nil {
			return fmt.Errorf("microvm: nat rule %v: %w", r, err)
		}
	}
	return nil
}

// allocIP reserves the next free guest IP in the /24.
func (n *vmNet) allocIP() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for o := ipFirstOctet; o <= ipLastOctet; o++ {
		if !n.used[o] {
			n.used[o] = true
			return ipForOctet(o), nil
		}
	}
	return "", fmt.Errorf("microvm: no free IP in %s", vmSubnet)
}

func (n *vmNet) freeIP(ip string) {
	if o := lastOctet(ip); o >= 0 {
		n.mu.Lock()
		delete(n.used, o)
		n.mu.Unlock()
	}
}

// createTap makes a tap device attached to the bridge, ready for Firecracker.
func (n *vmNet) createTap(tap string) error {
	_ = exec.Command("ip", "link", "del", tap).Run() // clear any stale device
	if out, err := exec.Command("ip", "tuntap", "add", "dev", tap, "mode", "tap").CombinedOutput(); err != nil {
		return fmt.Errorf("microvm: tap add %s: %v: %s", tap, err, out)
	}
	if out, err := exec.Command("ip", "link", "set", tap, "master", n.bridge).CombinedOutput(); err != nil {
		return fmt.Errorf("microvm: tap master: %v: %s", err, out)
	}
	if out, err := exec.Command("ip", "link", "set", tap, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("microvm: tap up: %v: %s", err, out)
	}
	return nil
}

func (n *vmNet) deleteTap(tap string) { _ = exec.Command("ip", "link", "del", tap).Run() }
