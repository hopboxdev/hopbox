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

// Resolver maps a caller's source IP to its box (read path).
type Resolver func(ctx context.Context, ip string) (*box.Box, error)

// Mutate resolves the calling box by IP, applies fn, and persists it (write
// path). One seam backs every owner command — boxd implements it as
// GetByIP -> fn -> Update.
type Mutate func(ctx context.Context, ip string, fn func(*box.Box)) error

// Server serves the metadata API.
type Server struct {
	resolve Resolver
	mutate  Mutate
	now     func() time.Time
}

// New builds the metadata server. mutate may be nil (writes are then no-ops).
func New(resolve Resolver, mutate Mutate) *Server {
	return &Server{resolve: resolve, mutate: mutate, now: time.Now}
}

// Handler returns the metadata routes (Go 1.22 method patterns).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("GET /v1/me/time", s.time)
	mux.HandleFunc("POST /v1/me/heartbeat", s.heartbeat)
	mux.HandleFunc("POST /v1/me/keep-alive", s.keepAlive)
	mux.HandleFunc("POST /v1/me/auto-suspend", s.autoSuspend)
	mux.HandleFunc("POST /v1/me/idle", s.idle)
	return mux
}

// meta is the /v1/me document — what a box can learn about itself.
type meta struct {
	Name           string  `json:"name"`
	Owner          string  `json:"owner"`
	Image          string  `json:"image"`
	IP             string  `json:"ip"`
	Phase          string  `json:"phase"`
	Ephemeral      bool    `json:"ephemeral"`
	MemMB          int64   `json:"mem_mb"`
	CPUMillis      int64   `json:"cpu_millis"`
	Load           float64 `json:"load"`
	Idle           bool    `json:"idle"`
	LastActive     int64   `json:"last_active"`
	AutoSuspend    bool    `json:"auto_suspend"`
	KeepAliveUntil int64   `json:"keep_alive_until"`
	IdleTimeoutSec int64   `json:"idle_timeout_sec"`
	StartedAt      int64   `json:"started_at"`
	ServerTimeNS   string  `json:"server_time_ns"`
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
	now := s.now()
	idle := b.EffectiveIdle(box.DefaultIdle)
	writeJSON(w, meta{
		Name: b.Name, Owner: b.Owner, Image: b.ImageRef, IP: b.IP, Phase: string(b.Phase),
		Ephemeral: b.Ephemeral, MemMB: b.MemMB, CPUMillis: b.CPUMillis,
		Load: b.Load, Idle: b.IsIdle(now, idle), LastActive: unix(b.LastActive),
		AutoSuspend: b.AutoSuspend, KeepAliveUntil: unix(b.KeepAliveUntil),
		IdleTimeoutSec: int64(idle.Timeout.Seconds()),
		StartedAt:      b.CreatedAt.Unix(), ServerTimeNS: nsString(now),
	})
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func (s *Server) time(w http.ResponseWriter, _ *http.Request) {
	now := s.now()
	writeJSON(w, map[string]any{"unix_secs": now.Unix(), "server_time_ns": nsString(now)})
}

// apply decodes the request body into req, then mutates the calling box via fn.
func apply[T any](s *Server, w http.ResponseWriter, r *http.Request, fn func(b *box.Box, req T)) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	var req T
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
	}
	if s.mutate == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.mutate(r.Context(), ip, func(b *box.Box) { fn(b, req) }); err != nil {
		http.Error(w, "unknown box for source "+r.RemoteAddr, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// heartbeat records a box's load report (F3).
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	type body struct {
		Load float64 `json:"load"`
	}
	now := s.now()
	apply(s, w, r, func(b *box.Box, req body) {
		b.RecordHeartbeat(req.Load, now, box.DefaultIdle)
	})
}

// keepAlive pins the box alive (no suspend) for a duration (default 5m).
func (s *Server) keepAlive(w http.ResponseWriter, r *http.Request) {
	type body struct {
		Duration string `json:"duration"`
	}
	now := s.now()
	apply(s, w, r, func(b *box.Box, req body) {
		d := 5 * time.Minute
		if req.Duration != "" {
			if p, err := time.ParseDuration(req.Duration); err == nil {
				d = p
			}
		}
		b.KeepAliveUntil = now.Add(d)
	})
}

// autoSuspend toggles whether the box auto-suspends when idle.
func (s *Server) autoSuspend(w http.ResponseWriter, r *http.Request) {
	type body struct {
		Enabled bool `json:"enabled"`
	}
	apply(s, w, r, func(b *box.Box, req body) { b.AutoSuspend = req.Enabled })
}

// idle sets the per-box idle timeout (0 clears it back to the daemon default).
func (s *Server) idle(w http.ResponseWriter, r *http.Request) {
	type body struct {
		Timeout string `json:"timeout"`
	}
	apply(s, w, r, func(b *box.Box, req body) {
		if req.Timeout == "" {
			b.IdleTimeoutOverride = 0
			return
		}
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			b.IdleTimeoutOverride = d
		}
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
