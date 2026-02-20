//go:build darwin

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const utunControlName = "com.apple.net.utun_control"

// CreateTUNSocket opens a macOS utun device and returns the fd as an *os.File.
// Requires root. The caller is responsible for closing the file.
func CreateTUNSocket() (*os.File, error) {
	syscall.ForkLock.RLock()
	fd, err := unix.Socket(unix.AF_SYSTEM, unix.SOCK_DGRAM, 2)
	if err != nil {
		syscall.ForkLock.RUnlock()
		return nil, fmt.Errorf("create AF_SYSTEM socket: %w", err)
	}
	unix.CloseOnExec(fd)
	syscall.ForkLock.RUnlock()

	ctlInfo := &unix.CtlInfo{}
	copy(ctlInfo.Name[:], []byte(utunControlName))
	if err := unix.IoctlCtlInfo(fd, ctlInfo); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("IoctlCtlInfo: %w", err)
	}

	sc := &unix.SockaddrCtl{
		ID:   ctlInfo.Id,
		Unit: 0, // auto-assign utun index
	}
	if err := unix.Connect(fd, sc); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("connect utun: %w", err)
	}

	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("set nonblock: %w", err)
	}

	return os.NewFile(uintptr(fd), "utun"), nil
}

// SetMTU sets the MTU on a utun interface. Requires root.
func SetMTU(ifName string, mtu int) error {
	if mtu <= 0 {
		return nil
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("socket for MTU ioctl: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()

	var ifr unix.IfreqMTU
	copy(ifr.Name[:], ifName)
	ifr.MTU = int32(mtu)
	if err := unix.IoctlSetIfreqMTU(fd, &ifr); err != nil {
		return fmt.Errorf("set MTU on %s: %w", ifName, err)
	}
	return nil
}

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
