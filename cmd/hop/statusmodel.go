package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// dashData holds all data displayed by the status TUI.
type dashData struct {
	// Host info
	hostName string
	endpoint string

	// Tunnel
	tunnelUp    bool
	connected   bool
	uptime      time.Duration
	lastHealthy time.Duration
	ping        time.Duration
	agentVer    string

	// Services
	services []svcInfo

	// Bridges (from manifest)
	bridges []bridgeInfo
}

type svcInfo struct {
	Name    string
	Type    string
	Running bool
	Error   string
}

type bridgeInfo struct {
	Type   string
	Active bool
}

// fetchDashData collects all dashboard data from local state + agent API.
func fetchDashData(hostName string, cfg *hostconfig.HostConfig) dashData {
	d := dashData{
		hostName: hostName,
		endpoint: cfg.Endpoint,
	}

	// 1. Tunnel state (local file).
	state, _ := tunnel.LoadState(hostName)
	if state == nil {
		return d
	}
	d.tunnelUp = true
	d.connected = state.Connected
	d.uptime = time.Since(state.StartedAt)
	if !state.LastHealthy.IsZero() {
		d.lastHealthy = time.Since(state.LastHealthy)
	}

	// 2. Health ping (measures round-trip).
	hostname := state.Hostname
	if hostname == "" {
		hostname = hostName + ".hop"
	}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)
	client := &http.Client{Timeout: 5 * time.Second}

	start := time.Now()
	resp, err := client.Get(agentURL)
	if err == nil {
		d.ping = time.Since(start)
		defer func() { _ = resp.Body.Close() }()

		var health map[string]any
		body, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(body, &health)
		if v, ok := health["version"]; ok {
			d.agentVer = fmt.Sprint(v)
		}
	}

	// 3. Services (RPC).
	svcResult, err := rpcclient.Call(hostName, "services.list", nil)
	if err == nil {
		var svcs []svcInfo
		if json.Unmarshal(svcResult, &svcs) == nil {
			d.services = svcs
		}
	}

	// 4. Bridges (from manifest — assumes active if tunnel is up).
	ws, err := manifest.Parse("hopbox.yaml")
	if err == nil {
		for _, b := range ws.Bridges {
			d.bridges = append(d.bridges, bridgeInfo{
				Type:   b.Type,
				Active: d.connected,
			})
		}
	}

	return d
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatDuration formats a duration as a compact human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, h)
}
