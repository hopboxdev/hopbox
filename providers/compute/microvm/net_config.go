package microvm

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// VM network layout. Each box is a tap on a host bridge; the bridge gateway is
// the host, where the agent hub + metadata API listen. Static IPs (kernel ip=)
// avoid needing a DHCP server in the VM.
const (
	vmBridge     = "hopbox-vmnet"
	vmGateway    = "10.0.0.1"
	vmNetmask    = "255.255.255.0"
	vmSubnet     = "10.0.0.0/24"
	vmBridgeCIDR = "10.0.0.1/24"
	ipFirstOctet = 2   // 10.0.0.2 is the first guest
	ipLastOctet  = 254 // 10.0.0.254 is the last
)

// ipForOctet returns the guest IP for a host octet in the /24.
func ipForOctet(octet int) string { return fmt.Sprintf("10.0.0.%d", octet) }

// lastOctet extracts the final octet of a 10.0.0.x address (-1 if not parseable).
func lastOctet(ip string) int {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return -1
	}
	return int(p[3])
}

// macFromIP builds a stable locally-administered MAC from an IPv4 — 06:00 (LAA,
// unicast) followed by the four address octets. The Firecracker convention.
func macFromIP(ip string) string {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return ""
	}
	return fmt.Sprintf("06:00:%02x:%02x:%02x:%02x", p[0], p[1], p[2], p[3])
}

// ipBootArg builds the kernel ip= parameter for static eth0 config (no DHCP):
// ip=<client>::<gateway>:<netmask>:<hostname>:<device>:<autoconf>.
func ipBootArg(ip, gateway, netmask string) string {
	return fmt.Sprintf("ip=%s::%s:%s::eth0:off", ip, gateway, netmask)
}

// tapNameForIP derives the host tap device name for a guest IP. Interface names
// are capped at 15 chars; "fctap" + octet (<= 8) is safe and 1:1 with the IP.
func tapNameForIP(ip string) string {
	return fmt.Sprintf("fctap%d", lastOctet(ip))
}

// tapOctets parses `ip -br link show` output for the octets of existing fctap<N>
// devices. On a boxd restart these belong to orphaned VMs, so their IPs must not
// be re-handed-out. Pure (the host call lives in the provider).
func tapOctets(ipLinkOutput string) []int {
	var octets []int
	for _, line := range strings.Split(ipLinkOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.SplitN(fields[0], "@", 2)[0] // strip a "fctap2@if7" suffix
		if !strings.HasPrefix(name, "fctap") {
			continue
		}
		if o, err := strconv.Atoi(strings.TrimPrefix(name, "fctap")); err == nil {
			octets = append(octets, o)
		}
	}
	return octets
}
