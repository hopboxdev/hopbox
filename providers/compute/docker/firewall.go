//go:build docker

package docker

import (
	"fmt"
	"log"

	"github.com/coreos/go-iptables/iptables"
)

// Chain names for the workspace egress fence. Dedicated chains make the whole
// thing idempotent (clear + re-add) and easy to reason about / remove.
const (
	fenceInChain  = "HOPBOX-WS-IN"  // box -> host (INPUT)
	fenceFwdChain = "HOPBOX-WS-FWD" // box -> routed (DOCKER-USER / FORWARD)
)

// fenceRules returns the egress policy for a workspace box, by chain. Boxes may
// reach the allowed host ports (agent hub, metadata API) and the public internet,
// but not the host's other services, the LAN, the tailnet, or link-local/metadata.
// This is a pure function so the policy is unit-testable without root.
func fenceRules(allowHostPorts []string) (in, fwd [][]string) {
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
		{"-d", "10.0.0.0/8", "-j", "DROP"},
		{"-d", "172.16.0.0/12", "-j", "DROP"}, // other docker nets + private
		{"-d", "192.168.0.0/16", "-j", "DROP"},
		{"-d", "100.64.0.0/10", "-j", "DROP"}, // tailscale CGNAT
		{"-j", "RETURN"},                      // everything else = public internet
	}
	return in, fwd
}

// ensureFence programs the egress firewall for the workspace subnet, idempotently.
// The daemon owns this — there is no script to run, and because it re-applies on
// startup/provision it survives reboots and docker restarts. Failures (no
// iptables, not root, non-Linux dev host) are logged, not fatal: isolation is
// best-effort hardening, not a reason to fail a box.
func (p *Provider) ensureFence(subnet string, allowHostPorts []string) {
	ipt, err := iptables.New()
	if err != nil {
		log.Printf("docker: workspace firewall unavailable (%v); boxes are NOT egress-fenced", err)
		return
	}
	in, fwd := fenceRules(allowHostPorts)
	if err := p.applyFence(ipt, "INPUT", fenceInChain, subnet, in); err != nil {
		log.Printf("docker: workspace firewall (INPUT): %v", err)
	}
	if err := p.applyFence(ipt, "DOCKER-USER", fenceFwdChain, subnet, fwd); err != nil {
		log.Printf("docker: workspace firewall (DOCKER-USER): %v", err)
	}
}

// applyFence (re)builds a dedicated chain with rules and hooks the subnet's
// traffic into it from parent, exactly once.
func (p *Provider) applyFence(ipt *iptables.IPTables, parent, chain, subnet string, rules [][]string) error {
	// ClearChain creates the chain if absent, else flushes it.
	if err := ipt.ClearChain("filter", chain); err != nil {
		return fmt.Errorf("clear %s: %w", chain, err)
	}
	for _, r := range rules {
		if err := ipt.Append("filter", chain, r...); err != nil {
			return fmt.Errorf("append %s %v: %w", chain, r, err)
		}
	}
	// Hook the workspace subnet into our chain from the parent, once.
	jump := []string{"-s", subnet, "-j", chain}
	if err := ipt.InsertUnique("filter", parent, 1, jump...); err != nil {
		return fmt.Errorf("hook %s->%s: %w", parent, chain, err)
	}
	return nil
}
