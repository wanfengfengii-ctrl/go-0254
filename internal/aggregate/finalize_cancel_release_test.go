package aggregate_test

import (
	"context"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

func TestModel_FinalizeCancellationReleasesLeases(t *testing.T) {
	tests := []struct {
		name         string
		advanceRoute bool
		cancelOpKey  string
	}{
		{name: "cancel from leases held", cancelOpKey: "cancel-leases-held"},
		{name: "cancel after route checking starts", advanceRoute: true, cancelOpKey: "cancel-route-checking"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc, _ := newService(t)

			cancelledMission, err := svc.LockMission(ctx, validRequest())
			if err != nil {
				t.Fatalf("lock cancelled mission: %v", err)
			}
			authorizeTwo(t, svc, cancelledMission)
			acquireLeases(t, svc, cancelledMission)
			if tt.advanceRoute {
				_, err = svc.SimulateSegment(ctx, cancelledMission.MissionID, 1, aggregate.SimulateRequest{
					Generation:         cancelledMission.Generation,
					OpKey:              "segment-before-cancel",
					SimulatorScriptRef: "sim-script-01",
					AdapterStatus:      domain.AttemptSuccess,
					Verdict:            domain.VerdictPass,
				})
				if err != nil {
					t.Fatalf("start route checking: %v", err)
				}
			}

			outcome, err := svc.Finalize(ctx, cancelledMission.MissionID, aggregate.FinalizeRequest{
				Generation: cancelledMission.Generation,
				OpKey:      tt.cancelOpKey,
				Result:     domain.FinalCancelled,
			})
			if err != nil {
				t.Fatalf("finalize cancellation: %v", err)
			}
			if outcome.State != domain.StateCancelled || outcome.TerminalResult != domain.FinalCancelled {
				t.Fatalf("final outcome = state %q result %q, want cancelled", outcome.State, outcome.TerminalResult)
			}

			detail, err := svc.GetMission(ctx, cancelledMission.MissionID)
			if err != nil {
				t.Fatalf("get cancelled mission: %v", err)
			}
			if detail.State != domain.StateCancelled || detail.TerminalResult != string(domain.FinalCancelled) {
				t.Errorf("persisted terminal state = state %q result %q, want cancelled", detail.State, detail.TerminalResult)
			}
			if len(detail.Leases) != 3 {
				t.Fatalf("persisted leases = %d, want 3", len(detail.Leases))
			}
			var openResources []string
			for _, lease := range detail.Leases {
				if lease.Status == domain.LeaseOpen {
					openResources = append(openResources, string(lease.ResourceType)+":"+lease.ResourceKey)
				}
				if lease.Status != domain.LeaseReleased || lease.ReleasedAtTick == 0 {
					t.Errorf("cancelled mission lease %s:%s persisted as status %q released_at_tick %d, want released with a release tick",
						lease.ResourceType, lease.ResourceKey, lease.Status, lease.ReleasedAtTick)
				}
			}

			replacement, err := svc.LockMission(ctx, validRequest())
			if err != nil {
				t.Fatalf("lock replacement mission: %v", err)
			}
			authorizeTwo(t, svc, replacement)
			leases, err := svc.AcquireLeases(ctx, replacement.MissionID, aggregate.LeaseRequest{
				Generation: replacement.Generation,
				OpKey:      "replacement-leases",
			})
			if err != nil {
				t.Fatalf("replacement mission must reacquire the cancelled mission's hull and beacon; persisted open leases %v: %v", openResources, err)
			}
			if leases.State != domain.StateLeasesHeld || len(leases.Leases) != 3 {
				t.Fatalf("replacement lease result = state %q leases %d, want leases_held with 3 leases", leases.State, len(leases.Leases))
			}
		})
	}
}
