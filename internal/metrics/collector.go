package metrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Collector polls Docker stats and updates per-box Prometheus metrics.
type Collector struct {
	cli      *client.Client
	interval time.Duration
	// key: containerID, value: (user, box) so we can delete stale metrics
	known map[string]boxKey
}

type boxKey struct {
	user string
	box  string
}

// NewCollector creates a stats collector that polls every 10 seconds.
func NewCollector(cli *client.Client) *Collector {
	return &Collector{
		cli:      cli,
		interval: 10 * time.Second,
		known:    make(map[string]boxKey),
	}
}

// Start runs the collector in a background loop until ctx is done.
func (c *Collector) Start(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()

	// Run once immediately so /metrics isn't empty at startup.
	c.collect(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	containers, err := c.cli.ContainerList(cctx, container.ListOptions{
		All: false, // running only
		Filters: filters.NewArgs(
			filters.Arg("label", "hopbox.profile-hash"),
		),
	})
	if err != nil {
		slog.Warn("metrics collector: list containers failed", "err", err)
		return
	}

	seen := make(map[string]boxKey, len(containers))

	for _, ct := range containers {
		user, box, ok := parseContainerName(ct.Names)
		if !ok {
			continue
		}
		seen[ct.ID] = boxKey{user: user, box: box}

		if err := c.updateOne(cctx, ct.ID, user, box); err != nil {
			slog.Debug("metrics collector: stats failed", "container", ct.ID[:12], "err", err)
		}
	}

	// Single source of truth for the gauge: whatever Docker says is
	// running right now. Replaces the previous Inc/Dec bookkeeping in
	// Manager, which drifted across hopboxd restarts because the gauge
	// reset to 0 while containers kept running.
	ContainersRunningTotal.Set(float64(len(seen)))

	// Delete metrics for containers that have gone away.
	for id, k := range c.known {
		if _, still := seen[id]; !still {
			DeleteBoxMetrics(k.user, k.box, id)
		}
	}
	c.known = seen
}

func parseContainerName(names []string) (user, box string, ok bool) {
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		if !strings.HasPrefix(n, "hopbox-") {
			continue
		}
		rest := strings.TrimPrefix(n, "hopbox-")
		// Format is hopbox-<user>-<box>; split on the LAST dash.
		i := strings.LastIndex(rest, "-")
		if i <= 0 || i == len(rest)-1 {
			continue
		}
		return rest[:i], rest[i+1:], true
	}
	return "", "", false
}

func (c *Collector) updateOne(ctx context.Context, id, user, box string) error {
	resp, err := c.cli.ContainerStats(ctx, id, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var s dockerStats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return err
	}

	cpuPct := calcCPUPercent(&s)
	BoxCPUPercent.WithLabelValues(user, box, id).Set(cpuPct)
	BoxMemoryBytes.WithLabelValues(user, box, id).Set(float64(s.MemoryStats.Usage))
	BoxMemoryLimitBytes.WithLabelValues(user, box, id).Set(float64(s.MemoryStats.Limit))

	var rx, tx uint64
	for _, n := range s.Networks {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	BoxNetworkRxBytes.WithLabelValues(user, box, id).Set(float64(rx))
	BoxNetworkTxBytes.WithLabelValues(user, box, id).Set(float64(tx))

	var rd, wr uint64
	for _, e := range s.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			rd += e.Value
		case "write":
			wr += e.Value
		}
	}
	BoxBlockReadBytes.WithLabelValues(user, box, id).Set(float64(rd))
	BoxBlockWriteBytes.WithLabelValues(user, box, id).Set(float64(wr))
	return nil
}

// dockerStats is a minimal subset of the Docker stats JSON we need.
// We decode directly instead of importing container.StatsResponse so the
// collector stays decoupled from Docker SDK field renames.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

func calcCPUPercent(s *dockerStats) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta <= 0 || sysDelta <= 0 || cpus == 0 {
		return 0
	}
	return (cpuDelta / sysDelta) * cpus * 100.0
}
