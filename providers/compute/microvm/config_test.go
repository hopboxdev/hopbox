package microvm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVcpusFromMillis(t *testing.T) {
	cases := map[int64]int{0: 1, -1: 1, 500: 1, 1000: 1, 1001: 2, 2000: 2, 2500: 3, 4000: 4}
	for milli, want := range cases {
		if got := vcpusFromMillis(milli); got != want {
			t.Errorf("vcpusFromMillis(%d) = %d, want %d", milli, got, want)
		}
	}
}

func TestBuildConfigDefaultsAndShape(t *testing.T) {
	cfg := buildConfig(VMSpec{KernelPath: "/k/vmlinux", RootfsPath: "/r/rootfs.ext4"})
	if cfg.BootSource.KernelImagePath != "/k/vmlinux" {
		t.Fatalf("kernel = %q", cfg.BootSource.KernelImagePath)
	}
	if cfg.BootSource.BootArgs != DefaultBootArgs {
		t.Fatalf("boot args = %q", cfg.BootSource.BootArgs)
	}
	if cfg.MachineConfig.VcpuCount != 1 || cfg.MachineConfig.MemSizeMib != defaultMemMB {
		t.Fatalf("defaults: vcpu=%d mem=%d", cfg.MachineConfig.VcpuCount, cfg.MachineConfig.MemSizeMib)
	}
	if len(cfg.Drives) != 1 || !cfg.Drives[0].IsRootDevice || cfg.Drives[0].PathOnHost != "/r/rootfs.ext4" {
		t.Fatalf("root drive wrong: %+v", cfg.Drives)
	}
	if cfg.NetworkIfaces != nil {
		t.Fatalf("no tap ⇒ no network interface, got %+v", cfg.NetworkIfaces)
	}
}

func TestBuildConfigInitAndEnvCmdline(t *testing.T) {
	cfg := buildConfig(VMSpec{
		KernelPath: "/k", RootfsPath: "/r",
		Init: "/sbin/hopbox-init",
		Env:  map[string]string{"HOPBOX_CONTROL_ADDR": "10.0.0.1:7777", "HOPBOX_AGENT_TOKEN": "tok"},
	})
	args := cfg.BootSource.BootArgs
	// init= present, and env appended as cmdline key=value (sorted -> deterministic).
	want := DefaultBootArgs + " init=/sbin/hopbox-init HOPBOX_AGENT_TOKEN=tok HOPBOX_CONTROL_ADDR=10.0.0.1:7777"
	if args != want {
		t.Fatalf("boot args =\n  %q\nwant\n  %q", args, want)
	}
}

func TestBuildConfigResourcesAndNet(t *testing.T) {
	cfg := buildConfig(VMSpec{
		KernelPath: "/k", RootfsPath: "/r", MemMB: 2048, VcpuCount: vcpusFromMillis(2000),
		TapDev: "fc-tap0", GuestMAC: "06:00:AC:10:00:02",
	})
	if cfg.MachineConfig.VcpuCount != 2 || cfg.MachineConfig.MemSizeMib != 2048 {
		t.Fatalf("resources: vcpu=%d mem=%d", cfg.MachineConfig.VcpuCount, cfg.MachineConfig.MemSizeMib)
	}
	if len(cfg.NetworkIfaces) != 1 || cfg.NetworkIfaces[0].HostDevName != "fc-tap0" || cfg.NetworkIfaces[0].IfaceID != "eth0" {
		t.Fatalf("net iface wrong: %+v", cfg.NetworkIfaces)
	}

	// Serializes to the keys Firecracker's --config-file expects.
	b, _ := json.Marshal(cfg)
	for _, key := range []string{`"boot-source"`, `"kernel_image_path"`, `"drives"`, `"is_root_device"`, `"machine-config"`, `"vcpu_count"`, `"mem_size_mib"`, `"network-interfaces"`, `"host_dev_name"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("config JSON missing %s: %s", key, b)
		}
	}
}
