package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/containers"
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

func DirectTCPIPHandler(mgr *containers.Manager) ssh.ChannelHandler {
	return func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, sshCtx ssh.Context) {
		var d directTCPIPData
		if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
			newChan.Reject(gossh.ConnectionFailed, "failed to parse forward data")
			return
		}

		containerID, ok := sshCtx.Value("container_id").(string)
		if !ok {
			newChan.Reject(gossh.ConnectionFailed, "no container for session")
			return
		}

		containerIP, err := mgr.ContainerIP(context.Background(), containerID)
		if err != nil {
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("container IP: %v", err))
			return
		}

		dest := RewriteDestination(d.DestAddr, containerIP)
		addr := net.JoinHostPort(dest, fmt.Sprintf("%d", d.DestPort))

		var dialer net.Dialer
		conn2, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err != nil {
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
