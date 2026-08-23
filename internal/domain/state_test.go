package domain_test

import (
	"testing"

	"abyssalauvreleasegate/internal/domain"
)

func TestStateIsTerminal(t *testing.T) {
	terminal := []domain.State{
		domain.StateLaunched,
		domain.StateEngineeringIsolation,
		domain.StateCancelled,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("state %q should be terminal", s)
		}
	}

	nonTerminal := []domain.State{
		domain.StatePendingLock,
		domain.StatePendingAuthorization,
		domain.StateLeasesHeld,
		domain.StateRouteChecking,
		domain.StateMarginChecking,
		domain.StatePendingVesselConfirmation,
		domain.StateLaunchable,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("state %q should not be terminal", s)
		}
	}
}

func TestStateValid(t *testing.T) {
	for _, s := range []domain.State{
		domain.StatePendingLock, domain.StatePendingAuthorization, domain.StateLeasesHeld,
		domain.StateRouteChecking, domain.StateMarginChecking, domain.StatePendingVesselConfirmation,
		domain.StateLaunchable, domain.StateLaunched, domain.StateEngineeringIsolation, domain.StateCancelled,
	} {
		if !s.Valid() {
			t.Errorf("state %q should be valid", s)
		}
	}
	if domain.State("bogus").Valid() {
		t.Error("bogus state should not be valid")
	}
}
