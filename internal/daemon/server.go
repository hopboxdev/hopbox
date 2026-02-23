package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// Handler is implemented by the daemon to respond to IPC requests.
type Handler interface {
	HandleStatus() *DaemonStatus
	HandleShutdown()
}

// Server listens on a Unix socket and dispatches requests to a Handler.
type Server struct {
	sockPath string
	handler  Handler
	listener net.Listener
	wg       sync.WaitGroup
}

// NewServer creates a new IPC server.
func NewServer(sockPath string, handler Handler) *Server {
	return &Server{sockPath: sockPath, handler: handler}
}

// Start begins accepting connections. Non-blocking — runs in background.
func (s *Server) Start() error {
	// Remove stale socket file if it exists.
	_ = os.Remove(s.sockPath)

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.sockPath, err)
	}
	s.listener = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go s.handle(conn)
		}
	}()
	return nil
}

// Stop closes the listener and removes the socket file.
func (s *Server) Stop() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
	_ = os.Remove(s.sockPath)
}

func (s *Server) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "invalid request"})
		return
	}

	switch req.Method {
	case "status":
		status := s.handler.HandleStatus()
		_ = json.NewEncoder(conn).Encode(Response{OK: true, State: status})
	case "shutdown":
		_ = json.NewEncoder(conn).Encode(Response{OK: true})
		s.handler.HandleShutdown()
	default:
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: fmt.Sprintf("unknown method: %s", req.Method)})
	}
}
