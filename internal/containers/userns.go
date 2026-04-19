package containers

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/client"
)

// devUIDInContainer is the uid assigned to the `dev` user inside hopbox
// containers (matches the base image's useradd and common-utils feature
// options).
const devUIDInContainer = 1000

// containerUserNSOffset returns the base offset that the given container's
// user-namespace applies. Containers started under Sysbox (or any runtime
// that configures a user-namespace) have a non-identity mapping — e.g.,
// "0 165536 65536", meaning container uid 0 lives at host uid 165536.
// Containers started under plain runc report an identity map or no map at
// all, in which case this returns 0 (container uid == host uid).
//
// Both uid and gid maps use the same format; the caller should read either
// one depending on context.
func containerUserNSOffset(ctx context.Context, cli *client.Client, containerID string, kind string) (uint32, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("inspect container: %w", err)
	}
	if inspect.State == nil || inspect.State.Pid <= 0 {
		return 0, fmt.Errorf("container has no running pid")
	}
	mapPath := fmt.Sprintf("/proc/%d/%s_map", inspect.State.Pid, kind)
	f, err := os.Open(mapPath)
	if err != nil {
		// If the /proc entry isn't readable (e.g. plain runc with no userns),
		// fall back to identity mapping. Not a hard error.
		if os.IsPermission(err) || os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open %s: %w", mapPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "<container-id> <host-id> <count>"
		var contID, hostID, count uint32
		if _, err := fmt.Sscanf(line, "%d %d %d", &contID, &hostID, &count); err != nil {
			continue
		}
		// We only care about the mapping that contains container uid 0.
		if contID == 0 {
			return hostID, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read %s: %w", mapPath, err)
	}
	// No uid-0 mapping found → identity map.
	return 0, nil
}

// chownHomeForContainer walks homePath on the HOST and chowns every entry to
// the host-side uid/gid that corresponds to the `dev` user inside the given
// container's user-namespace. Needed because:
//
//  1. hopboxd writes files into the bind-mounted home (e.g. the default
//     .devcontainer/devcontainer.json on first connect) as the hopbox
//     system user, whose uid/gid is outside the container's userns range.
//     Container's own `chown` inside the namespace cannot reach those
//     files.
//  2. Sysbox's default setup does not idmap-mount bind volumes, so files
//     written inside the container by dev (uid 1000) end up owned by
//     offset+1000 on the host, and files written on the host by any
//     other uid appear raw inside the container.
//
// This runs on every EnsureRunning call (not just on container create) so
// that any drift introduced by hopboxd writes between sessions is
// normalized on the next reconnect.
//
// Requires CAP_CHOWN and CAP_FOWNER on the hopboxd process — see the
// AmbientCapabilities stanza in deploy/hopboxd.service.
func chownHomeForContainer(ctx context.Context, cli *client.Client, homePath, containerID string) error {
	uidOffset, err := containerUserNSOffset(ctx, cli, containerID, "uid")
	if err != nil {
		return fmt.Errorf("uid offset: %w", err)
	}
	gidOffset, err := containerUserNSOffset(ctx, cli, containerID, "gid")
	if err != nil {
		return fmt.Errorf("gid offset: %w", err)
	}
	targetUID := int(uidOffset) + devUIDInContainer
	targetGID := int(gidOffset) + devUIDInContainer

	return filepath.WalkDir(homePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Tolerate transient errors while walking — best-effort cleanup.
			return nil
		}
		// Use Lchown so we don't follow symlinks.
		_ = os.Lchown(path, targetUID, targetGID)
		return nil
	})
}
