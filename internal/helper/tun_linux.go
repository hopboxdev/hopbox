//go:build linux

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.zx2c4.com/wireguard/tun"
)

// CreateTUNDevice creates a Linux kernel TUN device using wireguard-go.
// Returns the TUN fd as an *os.File and the interface name.
// Requires CAP_NET_ADMIN. The caller is responsible for closing the file.
func CreateTUNDevice(mtu int) (*os.File, string, error) {
	tunDev, err := tun.CreateTUN("hopbox%d", mtu)
	if err != nil {
		return nil, "", fmt.Errorf("create TUN: %w", err)
	}

	ifName, err := tunDev.Name()
	if err != nil {
		_ = tunDev.Close()
		return nil, "", fmt.Errorf("get TUN name: %w", err)
	}

	// File() is part of the tun.Device interface and returns the underlying
	// TUN file descriptor. The caller receives the same *os.File that the
	// Device holds internally; closing it will also tear down the device.
	return tunDev.File(), ifName, nil
}

// SetMTU sets the MTU on a TUN interface via the ip command.
func SetMTU(ifName string, mtu int) error {
	if mtu <= 0 {
		return nil
	}
	out, err := exec.Command("ip", "link", "set", ifName, "mtu", fmt.Sprintf("%d", mtu)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set MTU on %s: %w: %s", ifName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ipAddrAddArgs(iface, localCIDR string) []string {
	return []string{"addr", "add", localCIDR, "dev", iface}
}

func ipLinkUpArgs(iface string) []string {
	return []string{"link", "set", iface, "up"}
}

func ipRouteAddArgs(iface string) []string {
	return []string{"route", "add", "10.10.0.0/24", "dev", iface}
}

func ipRouteDelArgs(iface string) []string {
	return []string{"route", "del", "10.10.0.0/24", "dev", iface}
}

func ipLinkDelArgs(iface string) []string {
	return []string{"link", "delete", iface}
}

// ConfigureTUN assigns an IP to the interface and adds a route.
func ConfigureTUN(iface, localIP, peerIP string) error {
	localCIDR := localIP + "/24"
	if out, err := exec.Command("ip", ipAddrAddArgs(iface, localCIDR)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ip", ipLinkUpArgs(iface)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set up: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ip", ipRouteAddArgs(iface)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip route add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CleanupTUN removes the route and deletes the TUN interface.
// On Linux, TUN devices persist unless explicitly deleted (unlike macOS utun
// which is destroyed when the creating process closes its fd).
func CleanupTUN(iface string) error {
	_ = exec.Command("ip", ipRouteDelArgs(iface)...).Run()
	out, err := exec.Command("ip", ipLinkDelArgs(iface)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip link delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
