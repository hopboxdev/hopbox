//go:build firecracker

package microvm

import (
	"strings"
	"testing"
)

// TestVMFenceRulesPolicy pins the egress policy without root: the agent + metadata
// ports and the internet are reachable; the host's other services and the
// private/tailnet/link-local ranges are denied.
func TestVMFenceRulesPolicy(t *testing.T) {
	in, fwd := vmFenceRules([]string{"7780", "8091"})

	for _, port := range []string{"7780", "8091"} {
		if !hasRule(in, []string{"-p", "tcp", "--dport", port, "-j", "RETURN"}) {
			t.Fatalf("INPUT must allow host port %s: %v", port, in)
		}
	}
	if last := in[len(in)-1]; last[len(last)-1] != "DROP" {
		t.Fatalf("INPUT must end in DROP (no other host service): %v", last)
	}

	for _, cidr := range []string{"169.254.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10"} {
		if !hasRule(fwd, []string{"-d", cidr, "-j", "DROP"}) {
			t.Fatalf("FORWARD must drop %s: %v", cidr, fwd)
		}
	}
	if last := fwd[len(fwd)-1]; last[len(last)-1] != "ACCEPT" {
		t.Fatalf("FORWARD must end in ACCEPT (internet allowed): %v", last)
	}
}

func hasRule(rules [][]string, want []string) bool {
	for _, r := range rules {
		if strings.Join(r, " ") == strings.Join(want, " ") {
			return true
		}
	}
	return false
}
