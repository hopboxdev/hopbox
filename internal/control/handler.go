package control

import (
	"fmt"
	"log/slog"
	"runtime"
	"time"
)

// BoxInfo holds metadata about a box for the status command.
type BoxInfo struct {
	BoxName     string
	Username    string
	ContainerID string
	StartedAt   time.Time
	Hostname    string
	SSHPort     int
	Fingerprint string
}

// DestroyFunc is called by the destroy handler to clean up the container and box data.
type DestroyFunc func() error

// HandleRequest processes a control request and returns a response.
func HandleRequest(req Request, info BoxInfo, destroyFn DestroyFunc, linkStore *LinkStore) Response {
	switch req.Command {
	case "status":
		return handleStatus(info)
	case "destroy":
		return handleDestroy(req, info, destroyFn)
	case "link":
		return handleLink(info, linkStore)
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}

func handleLink(info BoxInfo, linkStore *LinkStore) Response {
	if linkStore == nil {
		return Response{OK: false, Error: "link codes not available"}
	}
	if info.Fingerprint == "" {
		return Response{OK: false, Error: "fingerprint not available"}
	}

	code, err := linkStore.GenerateCode(info.Fingerprint)
	if err != nil {
		return Response{OK: false, Error: fmt.Sprintf("generate link code: %v", err)}
	}

	return Response{OK: true, Data: map[string]string{"code": code}}
}

func handleStatus(info BoxInfo) Response {
	uptime := time.Since(info.StartedAt).Truncate(time.Second).String()

	sshPort := "2222"
	if info.SSHPort > 0 {
		sshPort = fmt.Sprintf("%d", info.SSHPort)
	}

	return Response{
		OK: true,
		Data: map[string]string{
			"box":      info.BoxName,
			"user":     info.Username,
			"os":       fmt.Sprintf("Ubuntu 24.04 (%s)", runtime.GOARCH),
			"uptime":   uptime,
			"hostname": info.Hostname,
			"ssh_port": sshPort,
		},
	}
}

func handleDestroy(req Request, info BoxInfo, destroyFn DestroyFunc) Response {
	if req.Confirm != info.BoxName {
		return Response{OK: false, Error: fmt.Sprintf("confirmation does not match box name %q", info.BoxName)}
	}

	slog.Info("destroying box", "component", "control", "box", info.BoxName, "container", info.ContainerID[:12])

	// Run the actual destruction asynchronously for two reasons:
	//  1. The hopbox CLI client lives inside the container we're about to
	//     stop — it must receive and print the OK response before the
	//     container goes away, or it hangs.
	//  2. Manager.DestroyBox calls SocketServer.Close(), which wg.Wait()s
	//     on in-flight handlers. This handler IS one of those in-flight
	//     handlers, so calling destroyFn synchronously would self-deadlock.
	go func() {
		// Small delay to ensure the response flushes to the client before
		// we start tearing the container down.
		time.Sleep(200 * time.Millisecond)
		if err := destroyFn(); err != nil {
			slog.Error("destroy failed", "component", "control", "box", info.BoxName, "err", err)
		}
	}()

	return Response{OK: true, Data: map[string]string{"destroyed": info.BoxName}}
}
