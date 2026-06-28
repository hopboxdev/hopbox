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
	d := DefaultNet()
	got := ipBootArg("10.0.0.7", d.gateway(), d.netmask())
	want := "ip=10.0.0.7::10.0.0.1:255.255.255.0::eth0:off"
	if got != want {
		t.Fatalf("ipBootArg = %q, want %q", got, want)
	}
}

func TestTapNameAndOctet(t *testing.T) {
	d := DefaultNet()
	if got := d.tapName("10.0.0.42"); got != "fctap42" {
		t.Fatalf("tapName = %q", got)
	}
	if len(d.tapName("10.0.0.254")) > 15 {
		t.Fatalf("tap name exceeds IFNAMSIZ: %q", d.tapName("10.0.0.254"))
	}
	if lastOctet("10.0.0.9") != 9 || lastOctet("nope") != -1 {
		t.Fatal("lastOctet wrong")
	}
	if d.ip(2) != "10.0.0.2" {
		t.Fatalf("ip(2) = %q", d.ip(2))
	}
}

// A non-default bridge derives a distinct subnet, tap prefix, and fence chain,
// and that prefix stays within the interface-name limit.
func TestNetConfigSecondFleet(t *testing.T) {
	a := NetConfig{Bridge: "hopbox-devnet", Subnet24: "10.1.0"}.withDefaults()
	if a.gateway() != "10.1.0.1" || a.bridgeCIDR() != "10.1.0.1/24" {
		t.Fatalf("subnet derivation wrong: gw=%s cidr=%s", a.gateway(), a.bridgeCIDR())
	}
	if a.TapPrefix == DefaultNet().TapPrefix {
		t.Fatalf("non-default bridge must not reuse the default tap prefix: %q", a.TapPrefix)
	}
	if a.FenceChain == DefaultNet().FenceChain {
		t.Fatalf("non-default bridge must not reuse the default fence chain: %q", a.FenceChain)
	}
	if len(a.tapName("10.1.0.254")) > 15 {
		t.Fatalf("derived tap name exceeds IFNAMSIZ: %q", a.tapName("10.1.0.254"))
	}
	// the default fleet is byte-for-byte unchanged.
	if d := DefaultNet().withDefaults(); d.TapPrefix != "fctap" || d.FenceChain != "HOPBOX-VM" {
		t.Fatalf("default fleet changed: %+v", d)
	}
}

func TestTapOctets(t *testing.T) {
	out := `lo               UNKNOWN        00:00:00:00:00:00 <LOOPBACK,UP>
eth0             UP             aa:bb:cc:dd:ee:ff <BROADCAST,MULTICAST,UP>
hopbox-vmnet     UP             d2:11:.. <BROADCAST,MULTICAST,UP>
fctap2@if9       UP             e6:.. <BROADCAST,MULTICAST,UP>
fctap17          UP             e6:.. <BROADCAST,MULTICAST,UP>
docker0          DOWN           02:.. <NO-CARRIER,BROADCAST,MULTICAST,UP>`
	got := tapOctets("fctap", out)
	want := map[int]bool{2: true, 17: true}
	if len(got) != 2 {
		t.Fatalf("tapOctets = %v, want octets 2 and 17", got)
	}
	for _, o := range got {
		if !want[o] {
			t.Fatalf("unexpected octet %d in %v", o, got)
		}
	}
}
