package box

type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseProvisioning Phase = "Provisioning"
	PhaseRunning      Phase = "Running"
	PhaseSuspended    Phase = "Suspended" // snapshotted to disk, resumable on attach
	PhaseStopped      Phase = "Stopped"
	PhaseFailed       Phase = "Failed"
	PhaseDestroying   Phase = "Destroying"
)

var transitions = map[Phase]map[Phase]bool{
	PhasePending:      {PhaseProvisioning: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseProvisioning: {PhaseRunning: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseRunning:      {PhaseProvisioning: true, PhaseSuspended: true, PhaseStopped: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseSuspended:    {PhaseRunning: true, PhaseProvisioning: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseStopped:      {PhaseProvisioning: true, PhaseDestroying: true},
	PhaseFailed:       {PhaseProvisioning: true, PhaseDestroying: true},
	PhaseDestroying:   {},
}

// CanTransition reports whether from->to is a legal phase change.
func CanTransition(from, to Phase) bool {
	if from == to {
		return true
	}
	return transitions[from][to]
}
