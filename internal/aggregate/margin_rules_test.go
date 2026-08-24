package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// lockToMargins locks a mission with the given request, authorizes it, captures
// leases, and accepts every segment so the mission is ready for a margin check.
func lockToMargins(t *testing.T, svc *aggregate.Aggregate, req aggregate.LockRequest) domain.Mission {
	t.Helper()
	m, err := svc.LockMission(context.Background(), req)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	authorizeTwo(t, svc, m)
	acquireLeases(t, svc, m)
	simulateAll(t, svc, m, len(req.Waypoints)-1)
	return m
}

func TestMarginExactThresholdPass(t *testing.T) {
	svc, _ := newService(t)
	m := lockToMargins(t, svc, validRequest())

	res, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation})
	if err != nil {
		t.Fatalf("margins: %v", err)
	}
	if !res.Passed {
		t.Fatalf("margins failed unexpectedly: %v", res.Reasons)
	}
	if res.Computation.EnergyUsedWh != 9000 {
		t.Errorf("energy_used = %d, want 9000", res.Computation.EnergyUsedWh)
	}
	if res.Computation.ReturnReserveWh != 41000 {
		t.Errorf("return_reserve = %d, want 41000", res.Computation.ReturnReserveWh)
	}
}

func TestMarginEnergyDeficit(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.Waypoints[0].ExpectedDrawWh = 30000
	req.Waypoints[1].ExpectedDrawWh = 30000
	m := lockToMargins(t, svc, req)

	res, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation})
	if err != nil {
		t.Fatalf("margins: %v", err)
	}
	if res.Passed {
		t.Fatal("energy deficit should fail margins")
	}
	if !containsReason(res.Reasons, "energy_deficit") {
		t.Fatalf("reasons = %v, want energy_deficit", res.Reasons)
	}
}

func TestMarginTrimHighBoundary(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.TrimMaxG = 30
	req.Waypoints[0].TargetDepthM = 4000
	req.Waypoints[1].TargetDepthM = 4000
	m := lockToMargins(t, svc, req)

	res, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation})
	if err != nil {
		t.Fatalf("margins: %v", err)
	}
	if res.Passed {
		t.Fatal("trim high should fail margins")
	}
	if !containsReason(res.Reasons, "trim_out_of_bounds") {
		t.Fatalf("reasons = %v, want trim_out_of_bounds", res.Reasons)
	}
}

func TestMarginTrimLowBoundary(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.TrimMinG = -30
	req.Waypoints[0].TargetDepthM = 0
	req.Waypoints[1].TargetDepthM = 0
	m := lockToMargins(t, svc, req)

	res, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation})
	if err != nil {
		t.Fatalf("margins: %v", err)
	}
	if res.Passed {
		t.Fatal("trim low should fail margins")
	}
	if !containsReason(res.Reasons, "trim_out_of_bounds") {
		t.Fatalf("reasons = %v, want trim_out_of_bounds", res.Reasons)
	}
}

func TestMarginDepthEnvelopeBreach(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.DepthLimitM = 3000
	req.Waypoints[1].TargetDepthM = 3500
	m := lockToMargins(t, svc, req)

	res, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation})
	if err != nil {
		t.Fatalf("margins: %v", err)
	}
	if res.Passed {
		t.Fatal("depth breach should fail margins")
	}
	if !containsReason(res.Reasons, "depth_envelope_breach") {
		t.Fatalf("reasons = %v, want depth_envelope_breach", res.Reasons)
	}
}

func TestMarginIntegerOverflow(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	huge := int64(1) << 62
	req.Waypoints[0].ExpectedDrawWh = huge
	req.Waypoints[1].ExpectedDrawWh = huge
	m := lockToMargins(t, svc, req)

	_, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation})
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeOverflow {
		t.Fatalf("code = %q, want overflow", de.Code)
	}
}

func containsReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}
