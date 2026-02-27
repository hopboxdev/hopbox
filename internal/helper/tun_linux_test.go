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
