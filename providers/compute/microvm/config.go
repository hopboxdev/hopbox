// Package microvm is the Firecracker microVM compute provider (F1). A box is a
// microVM, not a container — hardware isolation, and the substrate for
// suspend/snapshot/wake. It implements ports.Compute behind `//go:build
// firecracker`; this file is the pure, host-independent config generation so the
// mapping is unit-testable without KVM.
package microvm

const defaultMemMB = 256

// DefaultBootArgs boots to the rootfs init over the serial console. Networking
// and env are appended by the provider (F1.2/F1.3).
const DefaultBootArgs = "console=ttyS0 reboot=k panic=-1 pci=off"

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
	mem := s.MemMB
	if mem <= 0 {
		mem = defaultMemMB
	}
	vcpu := s.VcpuCount
	if vcpu < 1 {
		vcpu = 1
	}
	cfg := fcConfig{
		BootSource:    fcBootSource{KernelImagePath: s.KernelPath, BootArgs: args},
		Drives:        []fcDrive{{DriveID: "rootfs", PathOnHost: s.RootfsPath, IsRootDevice: true, IsReadOnly: false}},
		MachineConfig: fcMachineConfig{VcpuCount: vcpu, MemSizeMib: mem},
	}
	if s.TapDev != "" {
		cfg.NetworkIfaces = []fcNetIface{{IfaceID: "eth0", HostDevName: s.TapDev, GuestMAC: s.GuestMAC}}
	}
	return cfg
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
