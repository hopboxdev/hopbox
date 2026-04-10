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
		// -N closes the TCP connection after stdin EOF so half-close is
		// propagated; -w caps the connect timeout.
		ncCmd := []string{
			"nc",
			"-N",
			"-w", "10",
			dest,
			fmt.Sprintf("%d", d.DestPort),
		}

		exitCode, err := mgr.ExecNoTTY(context.Background(), containerID, ncCmd, nil, ch, ch, io.Discard)
		if err != nil {
			slog.Error("tunnel exec failed", "component", "tunnel", "target", dest, "port", d.DestPort, "err", err)
		} else if exitCode != 0 {
			slog.Debug("tunnel nc non-zero exit", "component", "tunnel", "code", exitCode, "target", dest, "port", d.DestPort)
		}
		ch.Close()
	}
}
