package gateway

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/containers"
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
func resolveContainerID(sshCtx ssh.Context, mgr *containers.Manager, store *users.Store, imageTag string) (string, error) {
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
	homePath := store.HomePath(fp, boxname)
	if err := os.MkdirAll(homePath, 0755); err != nil {
		return "", fmt.Errorf("create home dir: %w", err)
	}

	containerID, err := mgr.EnsureRunning(context.Background(), user.Username, boxname, imageTag, homePath)
	if err != nil {
		return "", err
	}

	sshCtx.SetValue("container_id", containerID)
	return containerID, nil
}

func DirectTCPIPHandler(mgr *containers.Manager, store *users.Store, imageTag string) ssh.ChannelHandler {
	return func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, sshCtx ssh.Context) {
		var d directTCPIPData
		if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
			newChan.Reject(gossh.ConnectionFailed, "failed to parse forward data")
			return
		}

		containerID, err := resolveContainerID(sshCtx, mgr, store, imageTag)
		if err != nil {
			log.Printf("[tunnel] resolve container failed: %v", err)
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

		log.Printf("[tunnel] %s:%d → %s (container %s)", d.DestAddr, d.DestPort, addr, containerID[:12])

		var dialer net.Dialer
		conn2, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err != nil {
			log.Printf("[tunnel] dial failed %s: %v", addr, err)
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
