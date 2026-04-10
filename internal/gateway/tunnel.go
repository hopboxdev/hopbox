package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/docker/client"
	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/control"
	"github.com/hopboxdev/hopbox/internal/users"
)

func RewriteDestination(host, containerIP string) string {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return containerIP
	default:
		return host
	}
}

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

		containerIP, err := mgr.ContainerIP(context.Background(), containerID)
		if err != nil {
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("container IP: %v", err))
			return
		}

		dest := RewriteDestination(d.DestAddr, containerIP)
		addr := net.JoinHostPort(dest, fmt.Sprintf("%d", d.DestPort))

		slog.Info("tunnel forward",
			"component", "tunnel",
			"dest_addr", d.DestAddr,
			"dest_port", d.DestPort,
			"target", addr,
			"container", containerID[:12])

		var dialer net.Dialer
		conn2, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err != nil {
			slog.Error("tunnel dial failed", "component", "tunnel", "target", addr, "err", err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("dial %s: %v", addr, err))
			return
		}

		ch, reqs, err := newChan.Accept()
		if err != nil {
			conn2.Close()
			return
		}
		go gossh.DiscardRequests(reqs)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			io.Copy(ch, conn2)
			ch.CloseWrite()
		}()
		go func() {
			defer wg.Done()
			io.Copy(conn2, ch)
			conn2.Close()
		}()
		wg.Wait()
		ch.Close()
	}
}
