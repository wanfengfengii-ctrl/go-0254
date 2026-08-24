package aggregate_test

import (
	"context"
	"reflect"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

func TestModel_AcquireLeasesIdempotentReplay(t *testing.T) {
	tests := []struct {
		name string
		req  aggregate.LeaseRequest
	}{
		{
			name: "byte-identical retry replays committed leases after state advances",
			req:  aggregate.LeaseRequest{Generation: 1, OpKey: "lease-retry-op-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newService(t)
			ctx := context.Background()

			m, err := svc.LockMission(ctx, validRequest())
			if err != nil {
				t.Fatalf("lock mission: %v", err)
			}
			authorizeTwo(t, svc, m)

			first, err := svc.AcquireLeases(ctx, m.MissionID, tt.req)
			if err != nil {
				t.Fatalf("first acquire leases: %v", err)
			}
			if first.State != domain.StateLeasesHeld {
				t.Fatalf("first state = %q, want %q", first.State, domain.StateLeasesHeld)
			}
			if len(first.Leases) != 3 {
				t.Fatalf("first leases = %d, want 3", len(first.Leases))
			}

			second, err := svc.AcquireLeases(ctx, m.MissionID, tt.req)
			if err != nil {
				t.Fatalf("byte-identical retry returned error: %v", err)
			}
			if !reflect.DeepEqual(second, first) {
				t.Fatalf("retry result mismatch:\nfirst:  %#v\nsecond: %#v", first, second)
			}
		})
	}
}
