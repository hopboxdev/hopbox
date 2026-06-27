package microvm

import "testing"

func TestMacFromIP(t *testing.T) {
	cases := map[string]string{
		"10.0.0.2":   "06:00:0a:00:00:02",
		"10.0.0.254": "06:00:0a:00:00:fe",
		"bad":        "",
	}
	for ip, want := range cases {
		if got := macFromIP(ip); got != want {
			t.Errorf("macFromIP(%q) = %q, want %q", ip, got, want)
		}
	}
}

func TestIPBootArg(t *testing.T) {
	got := ipBootArg("10.0.0.7", vmGateway, vmNetmask)
	want := "ip=10.0.0.7::10.0.0.1:255.255.255.0::eth0:off"
	if got != want {
		t.Fatalf("ipBootArg = %q, want %q", got, want)
	}
}

func TestTapNameAndOctet(t *testing.T) {
	if got := tapNameForIP("10.0.0.42"); got != "fctap42" {
		t.Fatalf("tapNameForIP = %q", got)
	}
	if len(tapNameForIP("10.0.0.254")) > 15 {
		t.Fatalf("tap name exceeds IFNAMSIZ: %q", tapNameForIP("10.0.0.254"))
	}
	if lastOctet("10.0.0.9") != 9 || lastOctet("nope") != -1 {
		t.Fatal("lastOctet wrong")
	}
	if ipForOctet(2) != "10.0.0.2" {
		t.Fatalf("ipForOctet(2) = %q", ipForOctet(2))
	}
}
