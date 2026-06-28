// Package microvm is the Firecracker microVM compute provider (F1). A box is a
// microVM, not a container — hardware isolation, and the substrate for
// suspend/snapshot/wake. It implements ports.Compute behind `//go:build
// firecracker`; this file is the pure, host-independent config generation so the
// mapping is unit-testable without KVM.
package microvm

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
)

const defaultMemMB = 256

// DefaultBootArgs boots to the rootfs init over the serial console. Networking
// and env are appended by the provider (F1.2/F1.3).
const DefaultBootArgs = "console=ttyS0 reboot=k panic=-1 pci=off"

// vmInit is the in-VM launcher (baked into the golden rootfs): it mounts the
// essentials and execs hopbox-agent, which inherits the HOPBOX_* env the kernel
// placed from the cmdline.
const vmInit = "/sbin/hopbox-init"

// VMSpec is the resolved input to one microVM: image paths, resources, and (once
// F1.2 lands) its tap device. It is what fcConfig is built from.
type VMSpec struct {
	KernelPath string
	RootfsPath string
	VcpuCount  int
	MemMB      int64
	BootArgs   string // "" = DefaultBootArgs
	TapDev     string // host tap device name; "" = no network (F1.1)
	GuestMAC   string
	Init       string            // init= path (the in-VM launcher); "" = rootfs default
	Env        map[string]string // injected via kernel cmdline key=value -> init's environment
	HomeDrive  string            // host path of a persistent home ext4 image -> /dev/vdb; "" = none
}

// fcConfig mirrors Firecracker's `--config-file` JSON.
type fcConfig struct {
	BootSource    fcBootSource    `json:"boot-source"`
	Drives        []fcDrive       `json:"drives"`
	MachineConfig fcMachineConfig `json:"machine-config"`
	NetworkIfaces []fcNetIface    `json:"network-interfaces,omitempty"`
}

type fcBootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type fcDrive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type fcMachineConfig struct {
	VcpuCount  int   `json:"vcpu_count"`
	MemSizeMib int64 `json:"mem_size_mib"`
}

type fcNetIface struct {
	IfaceID     string `json:"iface_id"`
	HostDevName string `json:"host_dev_name"`
	GuestMAC    string `json:"guest_mac,omitempty"`
}

// buildConfig turns a VMSpec into the Firecracker machine config.
func buildConfig(s VMSpec) fcConfig {
	args := s.BootArgs
	if args == "" {
		args = DefaultBootArgs
	}
	if s.Init != "" {
		args += " init=" + s.Init
	}
	// The kernel hands unrecognized key=value cmdline tokens to init's environment,
	// so this is how the agent gets HOPBOX_* without a DHCP/config-drive. Sorted
	// for a deterministic cmdline. Cmdline tokens can't carry spaces/newlines, so
	// only space-free values go inline; the FULL env (incl. the dev-env's CA +
	// authorized_keys, which have spaces) is packed base64 into HOPBOX_ENV64, which
	// the agent decodes at startup. The inline copy keeps space-free env working
	// for an older agent that doesn't know HOPBOX_ENV64.
	for _, k := range sortedKeys(s.Env) {
		if v := s.Env[k]; !strings.ContainsAny(v, " \t\n") {
			args += " " + k + "=" + v
		}
	}
	if len(s.Env) > 0 {
		if blob, err := json.Marshal(s.Env); err == nil {
			args += " HOPBOX_ENV64=" + base64.StdEncoding.EncodeToString(blob)
		}
	}
	mem := s.MemMB
	if mem <= 0 {
		mem = defaultMemMB
	}
	vcpu := s.VcpuCount
	if vcpu < 1 {
		vcpu = 1
	}
	drives := []fcDrive{{DriveID: "rootfs", PathOnHost: s.RootfsPath, IsRootDevice: true, IsReadOnly: false}}
	if s.HomeDrive != "" {
		// second virtio-blk drive -> /dev/vdb, the dev-env's persistent home.
		drives = append(drives, fcDrive{DriveID: "home", PathOnHost: s.HomeDrive, IsRootDevice: false, IsReadOnly: false})
	}
	cfg := fcConfig{
		BootSource:    fcBootSource{KernelImagePath: s.KernelPath, BootArgs: args},
		Drives:        drives,
		MachineConfig: fcMachineConfig{VcpuCount: vcpu, MemSizeMib: mem},
	}
	if s.TapDev != "" {
		cfg.NetworkIfaces = []fcNetIface{{IfaceID: "eth0", HostDevName: s.TapDev, GuestMAC: s.GuestMAC}}
	}
	return cfg
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// vcpusFromMillis maps a CPU milli-core cap to a whole vCPU count (Firecracker
// only does whole vCPUs): round up, minimum 1. 0/unlimited ⇒ 1.
func vcpusFromMillis(milli int64) int {
	if milli <= 0 {
		return 1
	}
	n := int((milli + 999) / 1000)
	if n < 1 {
		n = 1
	}
	return n
}
