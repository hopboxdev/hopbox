package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Business metrics.
var (
	UsersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_users_total",
		Help: "Total number of registered users.",
	})

	BoxesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_boxes_total",
		Help: "Total number of boxes across all users.",
	})

	ContainersRunningTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_containers_running_total",
		Help: "Number of currently running hopbox containers.",
	})

	ActiveSessionsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_active_sessions_total",
		Help: "Current number of active SSH sessions.",
	})

	BoxActiveSessions = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_active_sessions",
		Help: "Current number of active SSH sessions per box.",
	}, boxLabels)

	BoxConnectTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hopbox_box_connect_total",
		Help: "Total number of successful box connect events.",
	}, []string{"user", "box"})

	BuildDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "hopbox_build_duration_seconds",
		Help:    "Duration of per-user image builds in seconds.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s .. ~17min
	})
)

// Per-box resource metrics. Labels: user, box, container_id.
var boxLabels = []string{"user", "box", "container_id"}

var (
	BoxCPUPercent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_cpu_percent",
		Help: "CPU usage percentage for a box container.",
	}, boxLabels)

	BoxMemoryBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_memory_bytes",
		Help: "Current memory usage in bytes for a box container.",
	}, boxLabels)

	BoxMemoryLimitBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_memory_limit_bytes",
		Help: "Memory limit in bytes for a box container.",
	}, boxLabels)

	BoxNetworkRxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_network_rx_bytes",
		Help: "Total network bytes received by a box container.",
	}, boxLabels)

	BoxNetworkTxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_network_tx_bytes",
		Help: "Total network bytes transmitted by a box container.",
	}, boxLabels)

	BoxBlockReadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_block_read_bytes",
		Help: "Total block device bytes read by a box container.",
	}, boxLabels)

	BoxBlockWriteBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_block_write_bytes",
		Help: "Total block device bytes written by a box container.",
	}, boxLabels)
)

// DeleteBoxMetrics removes all per-box gauge series for the given labels.
// Called when a container disappears so metrics don't accumulate stale labels.
func DeleteBoxMetrics(user, box, containerID string) {
	BoxCPUPercent.DeleteLabelValues(user, box, containerID)
	BoxMemoryBytes.DeleteLabelValues(user, box, containerID)
	BoxMemoryLimitBytes.DeleteLabelValues(user, box, containerID)
	BoxNetworkRxBytes.DeleteLabelValues(user, box, containerID)
	BoxNetworkTxBytes.DeleteLabelValues(user, box, containerID)
	BoxBlockReadBytes.DeleteLabelValues(user, box, containerID)
	BoxBlockWriteBytes.DeleteLabelValues(user, box, containerID)
	BoxActiveSessions.DeleteLabelValues(user, box, containerID)
}
