package containers

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

// containerHomeMountPoint is where the per-box home is bind-mounted inside
// the container. Kept in sync with the HostConfig.Binds entry in
// manager.go.
const containerHomeMountPoint = "/home/dev"

// readUserNSOffset parses /proc/<pid>/<kind>_map and returns the host-side
// base offset for container uid/gid 0. A Sysbox-style non-identity map like
// "0 165536 65536" yields 165536; plain runc (identity map or no map
// readable) yields 0.
func readUserNSOffset(pid int, kind string) (uint32, error) {
	mapPath := fmt.Sprintf("/proc/%d/%s_map", pid, kind)
	f, err := os.Open(mapPath)
	if err != nil {
		// Missing or unreadable /proc entry → treat as identity map rather
		// than failing the whole chown walk.
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
		var contID, hostID, count uint32
		if _, err := fmt.Sscanf(line, "%d %d %d", &contID, &hostID, &count); err != nil {
			continue
		}
		if contID == 0 {
			return hostID, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read %s: %w", mapPath, err)
	}
	return 0, nil
}

// isBindMountIdmapped reports whether the mount at `mountPoint` inside the
// given pid's mount namespace carries the kernel's `idmapped` flag. Sysbox
// sets this when it uses idmapped mounts (kernel ≥ 5.12) to apply the
// container's userns shift at the VFS layer — in which case host-underlying
// uid X shows up inside the container as uid X directly, so the chown walk
// must NOT add the userns offset.
func isBindMountIdmapped(pid int, mountPoint string) (bool, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/mountinfo", pid))
	if err != nil {
		if os.IsPermission(err) || os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open mountinfo: %w", err)
	}
	defer f.Close()
	return parseMountInfoIdmapped(f, mountPoint)
}

// parseMountInfoIdmapped is the pure parser split out from
// isBindMountIdmapped so it can be unit-tested with a synthetic reader.
func parseMountInfoIdmapped(r io.Reader, mountPoint string) (bool, error) {
	target := filepath.Clean(mountPoint)
	scanner := bufio.NewScanner(r)
	// mountinfo lines can be long; give the scanner a generous buffer.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Minimum viable mountinfo line:
		//   0:id 1:parent 2:maj:min 3:root 4:mountpoint 5:opts 6:- 7:fstype 8:source 9:super-opts
		if len(fields) < 10 {
			continue
		}
		if filepath.Clean(fields[4]) != target {
			continue
		}
		for _, opt := range strings.Split(fields[5], ",") {
			if opt == "idmapped" {
				return true, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read mountinfo: %w", err)
	}
	return false, nil
}

// chownHomeForContainer walks homePath on the HOST and chowns every entry to
// the host-side uid/gid that, after the container's mount + userns
// translation, resolves to the `dev` user inside the container. Needed
// because hopboxd writes files into the bind-mounted home as the hopbox
// system user (uid 999 on the host) which the container cannot correct from
// inside — sysbox-root lacks CAP_CHOWN for uids outside its mapped range.
//
// Target uid selection depends on how Sysbox is shifting the bind mount on
// this kernel:
//
//   - Idmapped bind mount (kernel ≥ 5.12, Sysbox's default on modern
//     hosts): the mount layer applies the userns shift itself, so host
//     underlying uid X is seen inside the container as uid X. Target is
//     devUIDInContainer, no offset.
//   - No idmap (older kernels, or Sysbox configured to pre-chown on
//     container start): only the userns translates uids, so host
//     underlying uid (offset+1000) is what shows up as uid 1000 inside.
//     Target is offset + devUIDInContainer.
//
// Detection via /proc/<pid>/mountinfo.
//
// This runs on every EnsureRunning call (not just on container create) so
// any drift introduced by hopboxd writes between sessions is normalized on
// the next reconnect.
//
// Requires CAP_CHOWN and CAP_FOWNER on the hopboxd process — see the
// AmbientCapabilities stanza in deploy/hopboxd.service.
func chownHomeForContainer(ctx context.Context, cli *client.Client, homePath, containerID string) error {
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}
	if inspect.State == nil || inspect.State.Pid <= 0 {
		return fmt.Errorf("container has no running pid")
	}
	pid := inspect.State.Pid

	idmapped, err := isBindMountIdmapped(pid, containerHomeMountPoint)
	if err != nil {
		return fmt.Errorf("check idmap: %w", err)
	}

	var targetUID, targetGID int
	if idmapped {
		targetUID = devUIDInContainer
		targetGID = devUIDInContainer
	} else {
		uidOffset, err := readUserNSOffset(pid, "uid")
		if err != nil {
			return fmt.Errorf("uid offset: %w", err)
		}
		gidOffset, err := readUserNSOffset(pid, "gid")
		if err != nil {
			return fmt.Errorf("gid offset: %w", err)
		}
		targetUID = int(uidOffset) + devUIDInContainer
		targetGID = int(gidOffset) + devUIDInContainer
	}

	return filepath.WalkDir(homePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Tolerate transient errors while walking — best-effort cleanup.
			return nil
		}
		// Lchown so we don't follow symlinks out of the bind mount.
		_ = os.Lchown(path, targetUID, targetGID)
		return nil
	})
}
