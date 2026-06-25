package workspace

import "time"

// ReapAction is the pure outcome of evaluating a workspace's lifetime. The
// caller (the reconciler) performs the side effects: reap = drive to Destroying;
// SetDeadline / ClearDeadline = persist the new Deadline.
type ReapAction struct {
	Reap          bool       // box should be torn down now
	SetDeadline   *time.Time // persist this as the workspace Deadline (start of the grace countdown)
	ClearDeadline bool       // owner re-attached: cancel the pending reap
}

// EvalLifetime decides what should happen to a workspace at time now, based on
// its ephemerality, owner attachment, grace and hard cap. It is pure: it reads
// the workspace and returns an action, mutating nothing.
//
// The reap signal is Attached — whether an owner SSH session is currently held
// open — NOT AgentConnected. A box's agent stays connected for the container's
// whole life, so it tracks "box alive", not "owner present"; only the latter
// drives temporary-box teardown.
//
// Persistent workspaces (Ephemeral=false) are always a no-op here; their
// dead-agent handling is the reconciler's self-heal path, not reaping.
func (w *Workspace) EvalLifetime(now time.Time) ReapAction {
	if !w.Ephemeral {
		return ReapAction{}
	}
	// Hard cap wins unconditionally — a tier timeout reaps even an attached session.
	if w.MaxTTL > 0 && now.Sub(w.CreatedAt) >= w.MaxTTL {
		return ReapAction{Reap: true}
	}
	if w.Attached {
		if w.Deadline != nil {
			return ReapAction{ClearDeadline: true} // re-attached within grace: cancel reap
		}
		return ReapAction{}
	}
	// Detached.
	if w.Grace <= 0 {
		return ReapAction{Reap: true}
	}
	if w.Deadline == nil {
		d := now.Add(w.Grace)
		return ReapAction{SetDeadline: &d}
	}
	if !now.Before(*w.Deadline) {
		return ReapAction{Reap: true}
	}
	return ReapAction{}
}
