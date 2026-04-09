package control

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// SocketServer listens on a Unix socket and dispatches control commands.
type SocketServer struct {
	path      string
	listener  net.Listener
	info      BoxInfo
	destroyFn DestroyFunc
	done      chan struct{}
	wg        sync.WaitGroup
}

// NewSocketServer creates a socket server for a container.
func NewSocketServer(socketPath string, info BoxInfo, destroyFn DestroyFunc) (*SocketServer, error) {
	// Remove stale socket file
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	// Make socket world-writable so the dev user inside the container can access it
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		return nil, err
	}

	return &SocketServer{
		path:      socketPath,
		listener:  listener,
		info:      info,
		destroyFn: destroyFn,
		done:      make(chan struct{}),
	}, nil
}

// Serve starts accepting connections. Blocks until Close is called.
func (s *SocketServer) Serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return // intentional close
			default:
				log.Printf("[control] accept error on %s: %v", s.path, err)
				return
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *SocketServer) handleConn(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		resp := Response{OK: false, Error: "invalid request"}
		json.NewEncoder(conn).Encode(resp)
		return
	}

	resp := HandleRequest(req, s.info, s.destroyFn)
	json.NewEncoder(conn).Encode(resp)
}

// Close stops the socket server and cleans up.
func (s *SocketServer) Close() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
	os.Remove(s.path)
}

// SocketDir returns the host directory that holds the control socket for a container.
func SocketDir(containerName string) string {
	return "/tmp/hopbox-" + containerName
}

// SocketPath returns the path of the Unix socket on the host for a container.
func SocketPath(containerName string) string {
	return filepath.Join(SocketDir(containerName), "control.sock")
}
