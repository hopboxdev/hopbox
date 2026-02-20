package tunnel

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// ConnState represents the connectivity state of the agent.
type ConnState int

const (
	ConnStateConnected ConnState = iota
	ConnStateDisconnected
)

// ConnEvent is emitted by ConnMonitor when connectivity state changes.
type ConnEvent struct {
	State     ConnState
	At        time.Time
	DownSince time.Time     // zero when State == ConnStateConnected
	Duration  time.Duration // outage duration (only set on reconnect)
}

// MonitorConfig configures a ConnMonitor.
type MonitorConfig struct {
	HealthURL     string        // e.g. "http://10.10.0.2:4200/health"
	Client        *http.Client  // must use tun.DialContext to reach the agent
	Interval      time.Duration // default 5s
	Timeout       time.Duration // per-check timeout, default 3s
	FailThreshold int           // consecutive failures before declaring disconnected, default 2
	OnStateChange func(ConnEvent)
	OnHealthy     func(time.Time) // called on every successful check (including steady-state)
}

// ConnMonitor periodically checks agent health and reports state changes via OnStateChange.
type ConnMonitor struct {
	cfg         MonitorConfig
	mu          sync.RWMutex
	state       ConnState
	lastHealthy time.Time
	downSince   time.Time
	failCount   int
}

// NewConnMonitor creates a ConnMonitor with defaults applied.
// Assumes the initial state is connected (caller should have already verified reachability).
func NewConnMonitor(cfg MonitorConfig) *ConnMonitor {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.FailThreshold == 0 {
		cfg.FailThreshold = 2
	}
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	return &ConnMonitor{
		cfg:         cfg,
		state:       ConnStateConnected,
		lastHealthy: time.Now(),
	}
}

// Run starts the heartbeat loop. Blocks until ctx is cancelled.
func (m *ConnMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

// check performs a single health probe.
func (m *ConnMonitor) check(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, m.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, m.cfg.HealthURL, nil)
	if err != nil {
		m.recordFailure()
		return
	}
	resp, err := m.cfg.Client.Do(req)
	if err != nil {
		m.recordFailure()
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		m.recordFailure()
		return
	}
	m.recordSuccess()
}

func (m *ConnMonitor) recordFailure() {
	m.mu.Lock()
	m.failCount++
	var disconnectEvt *ConnEvent
	if m.failCount >= m.cfg.FailThreshold && m.state == ConnStateConnected {
		now := time.Now()
		m.state = ConnStateDisconnected
		m.downSince = now
		evt := ConnEvent{State: ConnStateDisconnected, At: now}
		disconnectEvt = &evt
	}
	m.mu.Unlock()

	if disconnectEvt != nil && m.cfg.OnStateChange != nil {
		m.cfg.OnStateChange(*disconnectEvt)
	}
}

func (m *ConnMonitor) recordSuccess() {
	m.mu.Lock()
	now := time.Now()
	m.lastHealthy = now
	m.failCount = 0

	var reconnectEvt *ConnEvent
	if m.state == ConnStateDisconnected {
		downSince := m.downSince
		m.state = ConnStateConnected
		m.downSince = time.Time{}
		evt := ConnEvent{State: ConnStateConnected, At: now, Duration: now.Sub(downSince)}
		reconnectEvt = &evt
	}
	m.mu.Unlock()

	if reconnectEvt != nil && m.cfg.OnStateChange != nil {
		m.cfg.OnStateChange(*reconnectEvt)
	}
	if m.cfg.OnHealthy != nil {
		m.cfg.OnHealthy(now)
	}
}

// State returns the current connectivity state and last-healthy timestamp.
func (m *ConnMonitor) State() (ConnState, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, m.lastHealthy
}
