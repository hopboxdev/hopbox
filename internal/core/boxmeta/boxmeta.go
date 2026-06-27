// Package boxmeta is the box metadata API — the control-plane endpoint a box
// reaches (cloud-metadata style) to learn about itself and tune its lifecycle.
// A box is identified by its **source IP**, so there is no credential in the box.
// The in-guest `box-guest` client is a thin HTTP wrapper over these endpoints.
//
// box-clean: serves box.Box data via a resolver; no dev-env dependency.
package boxmeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

// Resolver maps a caller's source IP to its box.
type Resolver func(ctx context.Context, ip string) (*box.Box, error)

// Server serves the metadata API.
type Server struct {
	resolve Resolver
	now     func() time.Time
}

// New builds the metadata server. now defaults to time.Now.
func New(resolve Resolver) *Server { return &Server{resolve: resolve, now: time.Now} }

// Handler returns the metadata routes (Go 1.22 method patterns).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("GET /v1/me/time", s.time)
	return mux
}

// meta is the /v1/me document — what a box can learn about itself.
type meta struct {
	Name         string `json:"name"`
	Owner        string `json:"owner"`
	Image        string `json:"image"`
	IP           string `json:"ip"`
	Phase        string `json:"phase"`
	Ephemeral    bool   `json:"ephemeral"`
	MemMB        int64  `json:"mem_mb"`
	CPUMillis    int64  `json:"cpu_millis"`
	StartedAt    int64  `json:"started_at"`
	ServerTimeNS string `json:"server_time_ns"`
}

func nsString(t time.Time) string { return fmt.Sprintf("%d.%09d", t.Unix(), t.Nanosecond()) }

// boxOf identifies the calling box by request source IP.
func (s *Server) boxOf(r *http.Request) (*box.Box, error) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return s.resolve(r.Context(), ip)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	b, err := s.boxOf(r)
	if err != nil {
		http.Error(w, "unknown box for source "+r.RemoteAddr, http.StatusNotFound)
		return
	}
	writeJSON(w, meta{
		Name: b.Name, Owner: b.Owner, Image: b.ImageRef, IP: b.IP, Phase: string(b.Phase),
		Ephemeral: b.Ephemeral, MemMB: b.MemMB, CPUMillis: b.CPUMillis,
		StartedAt: b.CreatedAt.Unix(), ServerTimeNS: nsString(s.now()),
	})
}

func (s *Server) time(w http.ResponseWriter, _ *http.Request) {
	now := s.now()
	writeJSON(w, map[string]any{"unix_secs": now.Unix(), "server_time_ns": nsString(now)})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
