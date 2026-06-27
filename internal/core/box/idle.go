package box

import "time"

// IdleConfig tunes idle detection: a box is idle when no owner session is
// attached and its load stays below LoadThreshold for at least Timeout.
type IdleConfig struct {
	Timeout       time.Duration
	LoadThreshold float64
}

// DefaultIdle is the default idle policy (15 minutes under low load).
var DefaultIdle = IdleConfig{Timeout: 15 * time.Minute, LoadThreshold: 0.2}

// RecordHeartbeat folds a load report into the box: it updates Load and, when the
// box is busy (attached or above the load threshold), refreshes the activity
// marker so the idle countdown only runs while the box is actually quiet.
func (b *Box) RecordHeartbeat(load float64, now time.Time, cfg IdleConfig) {
	b.Load = load
	if b.Attached || load >= cfg.LoadThreshold {
		b.LastActive = now
	}
}

// IsIdle reports whether the box has been quiet (unattached, low load) for at
// least cfg.Timeout. A box with no activity marker yet is not idle.
func (b *Box) IsIdle(now time.Time, cfg IdleConfig) bool {
	if b.Attached || b.Load >= cfg.LoadThreshold || b.LastActive.IsZero() {
		return false
	}
	return now.Sub(b.LastActive) >= cfg.Timeout
}
