//go:build firecracker

package microvm

import (
	"fmt"
	"log"

	"github.com/coreos/go-iptables/iptables"
)

// vmFenceRules is the egress policy for VM boxes, by chain. Boxes may reach the
// allowed host ports (agent hub + metadata) and the public internet, but not the
// host's other services, the LAN, the tailnet, or link-local/metadata ranges.
// Pure so the policy is unit-testable without root. (Mirrors the docker fence.)
func vmFenceRules(allowHostPorts []string) (in, fwd [][]string) {
	in = [][]string{
		{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN"},
	}
	for _, p := range allowHostPorts { // the control-plane ports on the host
		in = append(in, []string{"-p", "tcp", "--dport", p, "-j", "RETURN"})
	}
	in = append(in, []string{"-j", "DROP"}) // no other host service

	fwd = [][]string{
		{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN"},
		{"-d", "169.254.0.0/16", "-j", "DROP"}, // link-local / cloud metadata
		{"-d", "10.0.0.0/8", "-j", "DROP"},     // other private 10.x (the VM's own /24 is L2, not forwarded)
		{"-d", "172.16.0.0/12", "-j", "DROP"},  // other docker nets + private
		{"-d", "192.168.0.0/16", "-j", "DROP"},
		{"-d", "100.64.0.0/10", "-j", "DROP"}, // tailscale CGNAT
		{"-j", "ACCEPT"},                      // everything else = public internet
	}
	return in, fwd
}

// ensureFence programs the VM-subnet egress firewall idempotently. The daemon
// owns it (no script); re-applying on startup survives reboots. Failures (no
// iptables, not root) are logged, not fatal — best-effort hardening.
func (n *vmNet) ensureFence(allowHostPorts []string) {
	ipt, err := iptables.New()
	if err != nil {
		log.Printf("microvm: firewall unavailable (%v); VMs are NOT egress-fenced", err)
		return
	}
	in, fwd := vmFenceRules(allowHostPorts)
	if err := n.applyFence(ipt, "INPUT", n.cfg.fenceIn(), in); err != nil {
		log.Printf("microvm: firewall (INPUT): %v", err)
	}
	if err := n.applyFence(ipt, "FORWARD", n.cfg.fenceFwd(), fwd); err != nil {
		log.Printf("microvm: firewall (FORWARD): %v", err)
	}
}

// applyFence (re)builds a dedicated chain and hooks the bridge's traffic into it
// from parent, exactly once.
func (n *vmNet) applyFence(ipt *iptables.IPTables, parent, chain string, rules [][]string) error {
	if err := ipt.ClearChain("filter", chain); err != nil { // create-or-flush
		return fmt.Errorf("clear %s: %w", chain, err)
	}
	for _, r := range rules {
		if err := ipt.Append("filter", chain, r...); err != nil {
			return fmt.Errorf("append %s %v: %w", chain, r, err)
		}
	}
	jump := []string{"-i", n.cfg.Bridge, "-j", chain} // traffic from the VM bridge
	if err := ipt.InsertUnique("filter", parent, 1, jump...); err != nil {
		return fmt.Errorf("hook %s->%s: %w", parent, chain, err)
	}
	return nil
}
