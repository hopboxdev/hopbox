package control

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

// BoxInfo holds metadata about a box for the status command.
type BoxInfo struct {
	BoxName     string
	Username    string
	Shell       string
	Multiplexer string
	ContainerID string
	StartedAt   time.Time
}

// DestroyFunc is called by the destroy handler to clean up the container and box data.
type DestroyFunc func() error

// HandleRequest processes a control request and returns a response.
func HandleRequest(req Request, info BoxInfo, destroyFn DestroyFunc) Response {
	switch req.Command {
	case "status":
		return handleStatus(info)
	case "destroy":
		return handleDestroy(req, info, destroyFn)
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}

func handleStatus(info BoxInfo) Response {
	uptime := time.Since(info.StartedAt).Truncate(time.Second).String()

	return Response{
		OK: true,
		Data: map[string]string{
			"box":         info.BoxName,
			"user":        info.Username,
			"os":          fmt.Sprintf("Ubuntu 24.04 (%s)", runtime.GOARCH),
			"shell":       info.Shell,
			"multiplexer": info.Multiplexer,
			"uptime":      uptime,
		},
	}
}

func handleDestroy(req Request, info BoxInfo, destroyFn DestroyFunc) Response {
	if req.Confirm != info.BoxName {
		return Response{OK: false, Error: fmt.Sprintf("confirmation does not match box name %q", info.BoxName)}
	}

	log.Printf("[control] destroying box %s (container %s)", info.BoxName, info.ContainerID[:12])
	if err := destroyFn(); err != nil {
		return Response{OK: false, Error: fmt.Sprintf("destroy: %v", err)}
	}

	return Response{OK: true, Data: map[string]string{"destroyed": info.BoxName}}
}
