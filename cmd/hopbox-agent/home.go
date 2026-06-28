package main

import (
	"log"
	"os"
)

// mountHome mounts the persistent home block device at its target, for the
// microVM backend (HOPBOX_HOME_DEV=/dev/vdb, HOPBOX_HOME_MOUNT=/home/dev set by
// the provider). A no-op on docker/k8s, where the home is a bind mount and these
// vars are unset. Runs before loadSSHConfig so the SSH host key (kept under the
// home) persists on the volume. Uses the mount syscall (mountDev) — the agent is
// PID 1 with no PATH, so it can't exec /bin/mount.
func mountHome() {
	dev, target := os.Getenv("HOPBOX_HOME_DEV"), os.Getenv("HOPBOX_HOME_MOUNT")
	if dev == "" || target == "" {
		return
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		log.Printf("hopbox-agent: home mkdir %s: %v", target, err)
		return
	}
	if err := mountDev(dev, target); err != nil {
		log.Printf("hopbox-agent: mount home %s -> %s: %v", dev, target, err)
		return
	}
	log.Printf("hopbox-agent: mounted persistent home %s at %s", dev, target)
}
