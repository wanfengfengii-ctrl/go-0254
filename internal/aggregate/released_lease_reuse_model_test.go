package aggregate_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

func TestModel_CancelledMissionReleasedLeasesAllowResourceReuse(t *testing.T) {
	cases := []struct {
		name          string
		terminal      domain.FinalResult
		terminalState domain.State
	}{
		{
			name:          "cancelled mission releases persisted hull and beacon leases for the next mission",
			terminal:      domain.FinalCancelled,
			terminalState: domain.StateCancelled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "auvgate.db")
			var tick int64
			var id int
			openService := func(t *testing.T) (*aggregate.Aggregate, *store.SQLite) {
				t.Helper()
				st, err := store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("open sqlite: %v", err)
				}
				svc := aggregate.New(catalog.DefaultSeed(), st,
					func() int64 { tick++; return tick },
					func() string {
						id++
						return "M-" + itoa(int64(id))
					},
				)
				return svc, st
			}

			svc1, st1 := openService(t)
			first, err := svc1.LockMission(ctx, validRequest())
			if err != nil {
				t.Fatalf("lock first mission: %v", err)
			}
			authorizeTwo(t, svc1, first)
			if _, err := svc1.AcquireLeases(ctx, first.MissionID, aggregate.LeaseRequest{
				Generation: first.Generation,
				OpKey:      "lease-" + first.MissionID,
			}); err != nil {
				t.Fatalf("acquire first leases: %v", err)
			}
			outcome, err := svc1.Finalize(ctx, first.MissionID, aggregate.FinalizeRequest{
				Generation: first.Generation,
				OpKey:      "finalize-" + first.MissionID,
				Result:     tc.terminal,
			})
			if err != nil {
				t.Fatalf("finalize first mission: %v", err)
			}
			if outcome.State != tc.terminalState {
				t.Fatalf("terminal state = %q, want %q", outcome.State, tc.terminalState)
			}

			firstDetail, err := svc1.GetMission(ctx, first.MissionID)
			if err != nil {
				t.Fatalf("get first detail before restart: %v", err)
			}
			if firstDetail.State != tc.terminalState {
				t.Fatalf("detail state = %q, want %q", firstDetail.State, tc.terminalState)
			}
			if len(firstDetail.Leases) != 3 {
				t.Fatalf("first detail leases = %d, want 3", len(firstDetail.Leases))
			}
			for _, lease := range firstDetail.Leases {
				if lease.Status != domain.LeaseReleased {
					t.Fatalf("first detail lease %s/%s status = %q, want released", lease.ResourceType, lease.ResourceKey, lease.Status)
				}
				if lease.ReleasedAtTick == 0 {
					t.Fatalf("first detail lease %s/%s has no release tick", lease.ResourceType, lease.ResourceKey)
				}
			}
			if err := st1.Close(); err != nil {
				t.Fatalf("close first store: %v", err)
			}

			svc2, st2 := openService(t)
			defer st2.Close()
			persisted, err := svc2.GetMission(ctx, first.MissionID)
			if err != nil {
				t.Fatalf("get first detail after restart: %v", err)
			}
			if len(persisted.Leases) != 3 {
				t.Fatalf("persisted first leases = %d, want 3", len(persisted.Leases))
			}
			for _, lease := range persisted.Leases {
				if lease.Status != domain.LeaseReleased {
					t.Fatalf("persisted lease %s/%s status = %q, want released", lease.ResourceType, lease.ResourceKey, lease.Status)
				}
			}

			second, err := svc2.LockMission(ctx, validRequest())
			if err != nil {
				t.Fatalf("lock second mission: %v", err)
			}
			authorizeTwo(t, svc2, second)
			leaseResult, err := svc2.AcquireLeases(ctx, second.MissionID, aggregate.LeaseRequest{
				Generation: second.Generation,
				OpKey:      "lease-" + second.MissionID,
			})
			if err != nil {
				var de *domain.Error
				if errors.As(err, &de) {
					t.Fatalf("reacquire after persisted released leases returned %s reasons=%v; want success reusing HULL-01 and BEACON-01", de.Code, de.Reasons)
				}
				t.Fatalf("reacquire after persisted released leases: %v", err)
			}
			if leaseResult.State != domain.StateLeasesHeld {
				t.Fatalf("second mission state = %q, want leases_held", leaseResult.State)
			}
			if len(leaseResult.Leases) != 3 {
				t.Fatalf("second mission leases = %d, want 3", len(leaseResult.Leases))
			}
			for _, lease := range leaseResult.Leases {
				if lease.Status != domain.LeaseOpen {
					t.Fatalf("second mission lease %s/%s status = %q, want open", lease.ResourceType, lease.ResourceKey, lease.Status)
				}
			}
		})
	}
}
