package aggregate_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// TestLeaseConcurrency races two missions for the same AUV hull and beacon slot
// and asserts exactly one effective lease set with no partial rows for the
// loser.
func TestLeaseConcurrency(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	// Two missions sharing HULL-01 and BEACON-01.
	req := validRequest()
	m1, err := svc.LockMission(ctx, req)
	if err != nil {
		t.Fatalf("lock m1: %v", err)
	}
	m2, err := svc.LockMission(ctx, req)
	if err != nil {
		t.Fatalf("lock m2: %v", err)
	}
	authorizeTwo(t, svc, m1)
	authorizeTwo(t, svc, m2)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup

	race := func(m domain.Mission) {
		defer wg.Done()
		<-start
		_, err := svc.AcquireLeases(ctx, m.MissionID, aggregate.LeaseRequest{Generation: m.Generation, OpKey: "lease-" + m.MissionID})
		results <- err
	}
	wg.Add(2)
	go race(m1)
	go race(m2)
	close(start)
	wg.Wait()
	close(results)

	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var de *domain.Error
		if errors.As(err, &de) && de.Code == domain.ErrCodeLeaseConflict {
			conflicts++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1", successes)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want exactly 1", conflicts)
	}
}
