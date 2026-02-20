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
