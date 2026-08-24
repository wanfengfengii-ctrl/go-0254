package domain

// State is the lifecycle state of a deployment mission aggregate.
type State string

const (
	StatePendingLock               State = "pending_lock"
	StatePendingAuthorization      State = "pending_authorization"
	StateLeasesHeld                State = "leases_held"
	StateRouteChecking             State = "route_checking"
	StateMarginChecking            State = "margin_checking"
	StatePendingVesselConfirmation State = "pending_vessel_confirmation"
	StateLaunchable                State = "launchable"
	StateLaunched                  State = "launched"
	StateEngineeringIsolation      State = "engineering_isolation"
	StateCancelled                 State = "cancelled"
)

// IsTerminal reports whether the state is one of the three terminal outcomes.
// Terminal states reject all later ordinary mutations.
func (s State) IsTerminal() bool {
	switch s {
	case StateLaunched, StateEngineeringIsolation, StateCancelled:
		return true
	default:
		return false
	}
}

// Valid reports whether s is one of the documented mission states.
func (s State) Valid() bool {
	switch s {
	case StatePendingLock, StatePendingAuthorization, StateLeasesHeld,
		StateRouteChecking, StateMarginChecking, StatePendingVesselConfirmation,
		StateLaunchable, StateLaunched, StateEngineeringIsolation, StateCancelled:
		return true
	default:
		return false
	}
}
