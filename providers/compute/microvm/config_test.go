package microvm

import (
	"encoding/base64"
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
	ca := "ssh-ed25519 AAAA bob@host" // a space-containing value (the dev-env's CA)
	cfg := buildConfig(VMSpec{
		KernelPath: "/k", RootfsPath: "/r",
		Init: "/sbin/hopbox-init",
		Env: map[string]string{
			"HOPBOX_CONTROL_ADDR":    "10.0.0.1:7777",
			"HOPBOX_AGENT_TOKEN":     "tok",
			"HOPBOX_TRUSTED_USER_CA": ca,
		},
	})
	args := cfg.BootSource.BootArgs
	// init= + space-free env inline (sorted); the space-containing CA is NOT inline.
	if !strings.HasPrefix(args, DefaultBootArgs+" init=/sbin/hopbox-init HOPBOX_AGENT_TOKEN=tok HOPBOX_CONTROL_ADDR=10.0.0.1:7777 ") {
		t.Fatalf("inline cmdline wrong:\n  %q", args)
	}
	if strings.Contains(args, ca) {
		t.Fatalf("space-containing value leaked onto the cmdline:\n  %q", args)
	}
	// HOPBOX_ENV64 carries the full env (incl. the CA) base64-encoded, space-free.
	tok := ""
	for _, f := range strings.Fields(args) {
		if v, ok := strings.CutPrefix(f, "HOPBOX_ENV64="); ok {
			tok = v
		}
	}
	if tok == "" {
		t.Fatal("HOPBOX_ENV64 missing from cmdline")
	}
	raw, err := base64.StdEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("ENV64 decode: %v", err)
	}
	var env map[string]string
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("ENV64 parse: %v", err)
	}
	if env["HOPBOX_TRUSTED_USER_CA"] != ca || env["HOPBOX_AGENT_TOKEN"] != "tok" {
		t.Fatalf("ENV64 round-trip wrong: %+v", env)
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
