package box

import "time"

// ReapAction is the pure outcome of evaluating a box's lifetime. The caller (the
// reconciler) performs the side effects: reap = drive to Destroying; SetDeadline
// / ClearDeadline = persist the new Deadline.
type ReapAction struct {
	Reap          bool       // box should be torn down now
	SetDeadline   *time.Time // persist this as the Deadline (start of the grace countdown)
	ClearDeadline bool       // owner re-attached: cancel the pending reap
}

// EvalLifetime decides what should happen to a box at time now, based on its
// ephemerality, owner attachment, grace and hard cap. It is pure: it reads the
// box and returns an action, mutating nothing.
//
// The reap signal is Attached — whether an owner SSH session is currently held
// open — NOT AgentConnected. A box's agent stays connected for the container's
// whole life, so it tracks "box alive", not "owner present"; only the latter
// drives temporary-box teardown.
//
// Persistent boxes (Ephemeral=false) are always a no-op here; their dead-agent
// handling is the reconciler's self-heal path, not reaping.
func (b *Box) EvalLifetime(now time.Time) ReapAction {
	if !b.Ephemeral {
		return ReapAction{}
	}
	// Hard cap wins unconditionally — a tier timeout reaps even an attached session.
	if b.MaxTTL > 0 && now.Sub(b.CreatedAt) >= b.MaxTTL {
		return ReapAction{Reap: true}
	}
	if b.Attached {
		if b.Deadline != nil {
			return ReapAction{ClearDeadline: true} // re-attached within grace: cancel reap
		}
		return ReapAction{}
	}
	// Detached.
	if b.Grace <= 0 {
		return ReapAction{Reap: true}
	}
	if b.Deadline == nil {
		d := now.Add(b.Grace)
		return ReapAction{SetDeadline: &d}
	}
	if !now.Before(*b.Deadline) {
		return ReapAction{Reap: true}
	}
	return ReapAction{}
}
