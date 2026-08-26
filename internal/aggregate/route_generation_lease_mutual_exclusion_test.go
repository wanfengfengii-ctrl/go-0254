package aggregate_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

func TestModel_RouteGenerationLeaseMutualExclusion(t *testing.T) {
	tests := []struct {
		name         string
		firstHull    string
		firstBeacon  string
		secondHull   string
		secondBeacon string
	}{
		{
			name:         "same route with otherwise disjoint resources",
			firstHull:    "HULL-01",
			firstBeacon:  "BEACON-01",
			secondHull:   "HULL-02",
			secondBeacon: "BEACON-02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newService(t)
			ctx := context.Background()

			firstReq := validRequest()
			firstReq.AUVHullID = tt.firstHull
			firstReq.BeaconSlotID = tt.firstBeacon
			firstReq.DepthLimitM = 2800
			firstReq.TrimMinG = -250
			firstReq.TrimMaxG = 250
			firstReq.Waypoints[0].TargetDepthM = 2500
			firstReq.Waypoints[1].TargetDepthM = 2800

			secondReq := firstReq
			secondReq.AUVHullID = tt.secondHull
			secondReq.BeaconSlotID = tt.secondBeacon

			first, err := svc.LockMission(ctx, firstReq)
			if err != nil {
				t.Fatalf("lock first mission: %v", err)
			}
			second, err := svc.LockMission(ctx, secondReq)
			if err != nil {
				t.Fatalf("lock second mission: %v", err)
			}
			if first.RouteHash != second.RouteHash {
				t.Fatalf("identical waypoint packages produced different route hashes: %q and %q", first.RouteHash, second.RouteHash)
			}

			authorizeTwo(t, svc, first)
			authorizeTwo(t, svc, second)

			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, mission := range []domain.Mission{first, second} {
				mission := mission
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, acquireErr := svc.AcquireLeases(ctx, mission.MissionID, aggregate.LeaseRequest{
						Generation: mission.Generation,
						OpKey:      "route-lease-" + mission.MissionID,
					})
					results <- acquireErr
				}()
			}
			close(start)
			wg.Wait()
			close(results)

			successes, conflicts := 0, 0
			for acquireErr := range results {
				if acquireErr == nil {
					successes++
					continue
				}
				var domainErr *domain.Error
				if errors.As(acquireErr, &domainErr) && domainErr.Code == domain.ErrCodeLeaseConflict {
					conflicts++
					continue
				}
				t.Fatalf("unexpected acquire error: %v", acquireErr)
			}

			details := make([]domain.MissionDetail, 0, 2)
			for _, mission := range []domain.Mission{first, second} {
				detail, getErr := svc.GetMission(ctx, mission.MissionID)
				if getErr != nil {
					t.Fatalf("get mission %s: %v", mission.MissionID, getErr)
				}
				details = append(details, detail)
			}

			heldMissions, openLeases, openRouteLeases := 0, 0, 0
			routeKeys := make([]string, 0, 2)
			for _, detail := range details {
				if detail.State == domain.StateLeasesHeld {
					heldMissions++
				}
				for _, lease := range detail.Leases {
					if lease.Status == domain.LeaseOpen {
						openLeases++
					}
					if lease.ResourceType == domain.ResourceRouteGeneration && lease.Status == domain.LeaseOpen {
						openRouteLeases++
						routeKeys = append(routeKeys, lease.ResourceKey)
					}
				}
			}

			if successes != 1 || conflicts != 1 || heldMissions != 1 || openLeases != 3 || openRouteLeases != 1 {
				t.Fatalf("same route generation was not exclusive: successes=%d conflicts=%d leases_held=%d open_leases=%d open_route_leases=%d route_keys=%v states=[%s %s]",
					successes, conflicts, heldMissions, openLeases, openRouteLeases, routeKeys, details[0].State, details[1].State)
			}
		})
	}
}
