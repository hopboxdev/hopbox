//go:build darwin

package helper

import (
	"fmt"
	"os/exec"
	"strings"
)

func ifconfigArgs(iface, localIP, peerIP string) []string {
	return []string{iface, "inet", localIP, peerIP, "netmask", "255.255.255.0", "up"}
}

func routeAddArgs(iface string) []string {
	return []string{"-n", "add", "-net", "10.10.0.0/24", "-interface", iface}
}

func routeDelArgs() []string {
	return []string{"-n", "delete", "-net", "10.10.0.0/24"}
}

// ConfigureTUN assigns an IP to the interface and adds a route.
func ConfigureTUN(iface, localIP, peerIP string) error {
	if out, err := exec.Command("ifconfig", ifconfigArgs(iface, localIP, peerIP)...).CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("route", routeAddArgs(iface)...).CombinedOutput(); err != nil {
		return fmt.Errorf("route add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CleanupTUN removes the route. The utun device is destroyed when the
// creating process closes its file descriptor.
func CleanupTUN() error {
	out, err := exec.Command("route", routeDelArgs()...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
