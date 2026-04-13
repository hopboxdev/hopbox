package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/docker/docker/client"
	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/control"
	"github.com/hopboxdev/hopbox/internal/users"
)

type directTCPIPData struct {
	DestAddr string
	DestPort uint32
	SrcAddr  string
	SrcPort  uint32
}

// resolveContainerID returns the container ID for the current SSH connection.
// If the session handler already set it, use that. Otherwise (e.g. ssh -N -L),
// look up the user by fingerprint and ensure their container is running.
func resolveContainerID(sshCtx ssh.Context, mgr *containers.Manager, store *users.Store, dockerCli *client.Client, baseTag string, hostname string, sshPort int) (string, error) {
	if id, ok := sshCtx.Value("container_id").(string); ok && id != "" {
		return id, nil
	}

	fp, ok := sshCtx.Value("fingerprint").(string)
	if !ok {
		return "", fmt.Errorf("no fingerprint in session")
	}

	user, ok := store.LookupByFingerprint(fp)
	if !ok {
		return "", fmt.Errorf("unknown user")
	}

	_, boxname := ParseUsername(sshCtx.User())
	userDir := filepath.Join(store.Dir(), fp)

	profile, err := users.ResolveProfile(userDir, boxname)
	if err != nil {
		return "", fmt.Errorf("resolve profile: %w", err)
	}
	if profile == nil {
		p := users.DefaultProfile()
		profile = &p
	}

	imageTag, err := containers.EnsureUserImage(context.Background(), dockerCli, user.Username, *profile, baseTag)
	if err != nil {
		return "", fmt.Errorf("ensure image: %w", err)
	}

	homePath := store.HomePath(fp, boxname)
	if err := os.MkdirAll(homePath, 0755); err != nil {
		return "", fmt.Errorf("create home dir: %w", err)
	}

	profileHash := profile.Hash()
	boxInfo := control.BoxInfo{
		BoxName:     boxname,
		Username:    user.Username,
		Hostname:    hostname,
		SSHPort:     sshPort,
		Fingerprint: fp,
	}
	if profile != nil {
		boxInfo.Shell = profile.Shell.Tool
		boxInfo.Multiplexer = profile.Multiplexer.Tool
	}
	containerID, err := mgr.EnsureRunning(context.Background(), user.Username, boxname, imageTag, profileHash, homePath, boxInfo)
	if err != nil {
		return "", err
	}

	sshCtx.SetValue("container_id", containerID)
	return containerID, nil
}

type directStreamLocalData struct {
	SocketPath string
	Reserved0  string
	Reserved1  uint32
}

// DirectStreamLocalHandler handles direct-streamlocal@openssh.com channels,
// used by VSCode Remote SSH to forward a local TCP port to a Unix socket
// inside the container (e.g. the VSCode server socket).
func DirectStreamLocalHandler(mgr *containers.Manager, store *users.Store, dockerCli *client.Client, baseTag string, hostname string, sshPort int) ssh.ChannelHandler {
	return func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, sshCtx ssh.Context) {
		var d directStreamLocalData
		if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
			newChan.Reject(gossh.ConnectionFailed, "failed to parse streamlocal data")
			return
		}

		containerID, err := resolveContainerID(sshCtx, mgr, store, dockerCli, baseTag, hostname, sshPort)
		if err != nil {
			slog.Error("streamlocal resolve container failed", "component", "tunnel", "err", err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("resolve container: %v", err))
			return
		}

		slog.Info("streamlocal forward",
			"component", "tunnel",
			"socket", d.SocketPath,
			"container", containerID[:12])

		ch, reqs, err := newChan.Accept()
		if err != nil {
			return
		}
		go gossh.DiscardRequests(reqs)

		// Connect to the Unix socket inside the container using socat.
		// socat is more reliable than nc -U for bidirectional Unix socket I/O.
		// Fall back to nc -U if socat isn't available.
		cmd := []string{
			"sh", "-c",
			fmt.Sprintf(
				`if command -v socat >/dev/null 2>&1; then socat - UNIX-CONNECT:%s; else nc -U %s; fi`,
				d.SocketPath, d.SocketPath,
			),
		}

		exitCode, err := mgr.ExecNoTTY(sshCtx, containerID, cmd, nil, ch, ch, io.Discard)
		if err != nil {
			slog.Error("streamlocal exec failed", "component", "tunnel", "socket", d.SocketPath, "err", err)
		} else if exitCode != 0 {
			slog.Debug("streamlocal non-zero exit", "component", "tunnel", "code", exitCode, "socket", d.SocketPath)
		}
		ch.Close()
	}
}

func DirectTCPIPHandler(mgr *containers.Manager, store *users.Store, dockerCli *client.Client, baseTag string, hostname string, sshPort int) ssh.ChannelHandler {
	return func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, sshCtx ssh.Context) {
		var d directTCPIPData
		if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
			newChan.Reject(gossh.ConnectionFailed, "failed to parse forward data")
			return
		}

		containerID, err := resolveContainerID(sshCtx, mgr, store, dockerCli, baseTag, hostname, sshPort)
		if err != nil {
			slog.Error("tunnel resolve container failed", "component", "tunnel", "err", err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("resolve container: %v", err))
			return
		}

		// Normalize destination: if the client asked for localhost we want
		// the container's loopback, not the host's. Keeping it as 127.0.0.1
		// is correct here because we're about to exec nc inside the container,
		// so "localhost" refers to the container itself.
		dest := d.DestAddr
		switch dest {
		case "localhost", "0.0.0.0", "::1", "":
			dest = "127.0.0.1"
		}

		slog.Info("tunnel forward",
			"component", "tunnel",
			"dest_addr", d.DestAddr,
			"dest_port", d.DestPort,
			"target", fmt.Sprintf("%s:%d", dest, d.DestPort),
			"container", containerID[:12])

		ch, reqs, err := newChan.Accept()
		if err != nil {
			return
		}
		go gossh.DiscardRequests(reqs)

		// Reach the destination from inside the container by execing nc and
		// wiring its stdin/stdout to the ssh channel. This is the only way
		// to hit services bound to the container's loopback (e.g. the VSCode
		// remote server, which listens on 127.0.0.1 by design).
		//
		// -N (openbsd-netcat only; provided by netcat-openbsd in the base
		// image) half-closes the socket after stdin EOF so the remote side
		// sees a clean end-of-stream. -w caps the inactivity timeout.
		ncCmd := []string{
			"nc",
			"-N",
			"-w", "10",
			dest,
			fmt.Sprintf("%d", d.DestPort),
		}

		// Use the ssh context so cancellation propagates if the session
		// dies before nc finishes (rather than stranding exec goroutines).
		exitCode, err := mgr.ExecNoTTY(sshCtx, containerID, ncCmd, nil, ch, ch, io.Discard)
		if err != nil {
			slog.Error("tunnel exec failed", "component", "tunnel", "target", dest, "port", d.DestPort, "err", err)
		} else if exitCode != 0 {
			slog.Debug("tunnel nc non-zero exit", "component", "tunnel", "code", exitCode, "target", dest, "port", d.DestPort)
		}
		ch.Close()
	}
}
