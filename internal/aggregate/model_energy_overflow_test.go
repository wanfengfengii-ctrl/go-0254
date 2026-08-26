package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

func TestModel_CheckMarginsRejectsThreeWaypointEnergyOverflow(t *testing.T) {
	tests := []struct {
		name string
		draw int64
	}{
		{name: "three large draws wrap to a plausible negative total", draw: int64(1) << 62},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newService(t)
			req := validRequest()
			req.Waypoints = append(req.Waypoints, aggregate.WaypointInput{
				Seq:               3,
				LatitudeMicrodeg:  -12400000,
				LongitudeMicrodeg: 4540000,
				TargetDepthM:      3500,
				MaxSpeedCmS:       120,
				ExpectedDrawWh:    tt.draw,
				DwellSeconds:      60,
			})
			for i := range req.Waypoints {
				req.Waypoints[i].ExpectedDrawWh = tt.draw
			}
			mission := lockToMargins(t, svc, req)

			result, err := svc.CheckMargins(context.Background(), mission.MissionID, aggregate.MarginRequest{
				Generation: mission.Generation,
				OpKey:      "overflow-margin-check",
			})
			var domainErr *domain.Error
			if !errors.As(err, &domainErr) {
				t.Errorf("CheckMargins error = %v, want *domain.Error", err)
			} else {
				if domainErr.Code != domain.ErrCodeOverflow {
					t.Errorf("error code = %q, want %q", domainErr.Code, domain.ErrCodeOverflow)
				}
				if domainErr.Operation != "margins_check" {
					t.Errorf("error operation = %q, want margins_check", domainErr.Operation)
				}
				if len(domainErr.Reasons) != 1 || domainErr.Reasons[0] != "integer_overflow" {
					t.Errorf("error reasons = %v, want [integer_overflow]", domainErr.Reasons)
				}
			}
			if result.Computation.ReturnReserveWh != 0 {
				t.Errorf("returned reserve = %d, want no partial computation after overflow", result.Computation.ReturnReserveWh)
			}

			detail, getErr := svc.GetMission(context.Background(), mission.MissionID)
			if getErr != nil {
				t.Fatalf("GetMission: %v", getErr)
			}
			if detail.State != domain.StateMarginChecking {
				t.Errorf("persisted state = %q, want %q", detail.State, domain.StateMarginChecking)
			}
			if len(detail.MarginEvidence) != 0 {
				t.Errorf("persisted margin evidence = %v, want none after overflow", detail.MarginEvidence)
			}
		})
	}
}
