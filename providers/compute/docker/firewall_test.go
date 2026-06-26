//go:build docker

package docker

import (
	"strings"
	"testing"
)

// TestFenceRulesPolicy pins the egress policy without needing root or iptables:
// the agent hub port and the internet are reachable; the host's other services
// and private/tailnet ranges are denied.
func TestFenceRulesPolicy(t *testing.T) {
	in, fwd := fenceRules("7777")

	// INPUT (box -> host): only the agent port is allowed, everything else dropped.
	joinedIn := join(in)
	if !contains(in, []string{"-p", "tcp", "--dport", "7777", "-j", "RETURN"}) {
		t.Fatalf("INPUT must allow the agent hub port: %v", joinedIn)
	}
	if last := in[len(in)-1]; last[len(last)-1] != "DROP" {
		t.Fatalf("INPUT must end in DROP (no other host service): %v", last)
	}

	// FORWARD: private ranges + tailnet denied; a final RETURN allows the internet.
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16"} {
		if !contains(fwd, []string{"-d", cidr, "-j", "DROP"}) {
			t.Fatalf("FORWARD must drop %s: %v", cidr, join(fwd))
		}
	}
	if last := fwd[len(fwd)-1]; last[len(last)-1] != "RETURN" {
		t.Fatalf("FORWARD must end in RETURN (internet allowed): %v", last)
	}
}

func contains(rules [][]string, want []string) bool {
	for _, r := range rules {
		if strings.Join(r, " ") == strings.Join(want, " ") {
			return true
		}
	}
	return false
}

func join(rules [][]string) string {
	var b []string
	for _, r := range rules {
		b = append(b, strings.Join(r, " "))
	}
	return strings.Join(b, " | ")
}
