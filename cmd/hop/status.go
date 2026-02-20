package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/version"
)

// StatusCmd shows tunnel and workspace health.
type StatusCmd struct{}

func (c *StatusCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "HOST\t%s\n", cfg.Name)
	_, _ = fmt.Fprintf(tw, "ENDPOINT\t%s\n", cfg.Endpoint)
	_, _ = fmt.Fprintf(tw, "AGENT-IP\t%s\n", cfg.AgentIP)
	_, _ = fmt.Fprintf(tw, "CLIENT-VERSION\t%s\n", version.Version)

	state, _ := tunnel.LoadState(hostName)
	if state == nil {
		_, _ = fmt.Fprintf(tw, "TUNNEL\tdown\n")
		_, _ = fmt.Fprintf(tw, "AGENT\tnot reachable (tunnel is not running)\n")
		_ = tw.Flush()
		return nil
	}

	healthAddr := state.AgentAPIAddr
	if healthAddr == "" {
		healthAddr = fmt.Sprintf("%s:%d", cfg.AgentIP, tunnel.AgentAPIPort)
	}
	agentURL := "http://" + healthAddr + "/health"
	healthClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := healthClient.Get(agentURL)
	if err != nil {
		_, _ = fmt.Fprintf(tw, "AGENT\tunreachable: %v\n", err)
		_ = tw.Flush()
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	var health map[string]any
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &health)

	tunnelStatus := "down"
	if v, ok := health["tunnel"]; ok {
		if b, ok := v.(bool); ok && b {
			tunnelStatus = "up"
		}
	}
	agentStatus := "ok"
	if v, ok := health["status"]; ok {
		agentStatus = fmt.Sprint(v)
	}
	_, _ = fmt.Fprintf(tw, "TUNNEL\t%s\n", tunnelStatus)
	_, _ = fmt.Fprintf(tw, "AGENT\t%s\n", agentStatus)
	if v, ok := health["version"]; ok {
		_, _ = fmt.Fprintf(tw, "AGENT-VERSION\t%s\n", v)
	}
	_ = tw.Flush()

	// Fetch and display services.
	svcResult, err := rpcclient.Call(hostName, "services.list", nil)
	if err == nil {
		var svcs []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Running bool   `json:"running"`
			Error   string `json:"error,omitempty"`
		}
		if json.Unmarshal(svcResult, &svcs) == nil && len(svcs) > 0 {
			fmt.Println("\nSERVICES")
			sw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintf(sw, "  NAME\tTYPE\tSTATUS\n")
			for _, s := range svcs {
				status := "stopped"
				if s.Running {
					status = "running"
				}
				if s.Error != "" {
					status = "error: " + s.Error
				}
				_, _ = fmt.Fprintf(sw, "  %s\t%s\t%s\n", s.Name, s.Type, status)
			}
			_ = sw.Flush()
		}
	}
	return nil
}
