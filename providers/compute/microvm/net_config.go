package microvm

import (
	"fmt"
	"hash/fnv"
	"net"
	"strconv"
	"strings"
)

// NetConfig is the host network for a microVM fleet: a bridge + a /24 + the tap
// and iptables-chain names. Making it configurable lets two daemons (e.g. boxd
// and a dev-env hopboxd) run side by side on different bridges/subnets without
// colliding on IPs, tap devices, or fence chains. Each box is a tap on the
// bridge; the bridge gateway (.1) is the host, where the agent hub + metadata API
// listen. Static IPs (kernel ip=) avoid needing a DHCP server in the VM.
type NetConfig struct {
	Bridge     string // host bridge device
	Subnet24   string // first three octets of the /24 (gateway is .1)
	TapPrefix  string // per-fleet tap device prefix (tap = prefix+octet, <= IFNAMSIZ); "" derives from Bridge
	FenceChain string // iptables fence chain prefix (-IN/-FWD appended); "" derives from Bridge
}

// DefaultNet is boxd's original network — the values used before this was
// configurable, so the default fleet is byte-for-byte unchanged.
func DefaultNet() NetConfig {
	return NetConfig{Bridge: "hopbox-vmnet", Subnet24: "10.0.0", TapPrefix: "fctap", FenceChain: "HOPBOX-VM"}
}

// withDefaults fills blanks. The default bridge keeps the exact default tap/chain
// names; any other bridge derives distinct ones, so just setting Bridge + Subnet24
// is enough to stand up a second, non-colliding fleet.
func (c NetConfig) withDefaults() NetConfig {
	d := DefaultNet()
	if c.Bridge == "" {
		c.Bridge = d.Bridge
	}
	if c.Subnet24 == "" {
		c.Subnet24 = d.Subnet24
	}
	if c.TapPrefix == "" {
		c.TapPrefix = d.TapPrefix
		if c.Bridge != d.Bridge {
			c.TapPrefix = tapTag(c.Bridge) // short + unique per bridge
		}
	}
	if c.FenceChain == "" {
		c.FenceChain = d.FenceChain
		if c.Bridge != d.Bridge {
			c.FenceChain = chainTag(c.Bridge)
		}
	}
	return c
}

const (
	ipFirstOctet = 2   // .2 is the first guest
	ipLastOctet  = 254 // .254 is the last
)

func (c NetConfig) gateway() string     { return c.Subnet24 + ".1" }
func (c NetConfig) netmask() string     { return "255.255.255.0" }
func (c NetConfig) subnet() string      { return c.Subnet24 + ".0/24" }
func (c NetConfig) bridgeCIDR() string  { return c.Subnet24 + ".1/24" }
func (c NetConfig) ip(octet int) string { return fmt.Sprintf("%s.%d", c.Subnet24, octet) }
func (c NetConfig) fenceIn() string     { return c.FenceChain + "-IN" }
func (c NetConfig) fenceFwd() string    { return c.FenceChain + "-FWD" }

// tapName derives the host tap device name for a guest IP. Interface names are
// capped at 15 chars; the derived prefix stays short enough that prefix+octet fits.
func (c NetConfig) tapName(ip string) string { return fmt.Sprintf("%s%d", c.TapPrefix, lastOctet(ip)) }

// tapTag derives a short, bridge-unique tap prefix (e.g. "vt1a2b") for a
// non-default fleet, so two daemons never share a tap device name.
func tapTag(bridge string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(bridge))
	return fmt.Sprintf("vt%04x", h.Sum32()&0xffff)
}

// chainTag derives an iptables-safe fence chain prefix from a bridge name.
func chainTag(bridge string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 32
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return r
		default:
			return '-'
		}
	}, bridge)
}

// lastOctet extracts the final octet of an IPv4 address (-1 if not parseable).
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

// tapOctets parses `ip -br link show` output for the octets of existing
// <prefix><N> tap devices. On a restart these belong to orphaned VMs, so their
// IPs must not be re-handed-out. Pure (the host call lives in the provider).
func tapOctets(prefix, ipLinkOutput string) []int {
	var octets []int
	for _, line := range strings.Split(ipLinkOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.SplitN(fields[0], "@", 2)[0] // strip a "vt0a2b2@if7" suffix
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if o, err := strconv.Atoi(strings.TrimPrefix(name, prefix)); err == nil {
			octets = append(octets, o)
		}
	}
	return octets
}
