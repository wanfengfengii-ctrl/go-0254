package aggregate_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// toLaunchable drives a mission through the full prerequisite chain so it is
// ready for the final arbiter.
func toLaunchable(t *testing.T, svc *aggregate.Aggregate, req aggregate.LockRequest) domain.Mission {
	t.Helper()
	m := lockToMargins(t, svc, req)
	if _, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation}); err != nil {
		t.Fatalf("margins: %v", err)
	}
	if _, err := svc.RecordVesselConfirmation(context.Background(), m.MissionID, aggregate.VesselRequest{
		Generation: m.Generation, AdapterStatus: domain.AttemptSuccess, ResponseHash: "reply",
	}); err != nil {
		t.Fatalf("vessel: %v", err)
	}
	reviewAll(t, svc, m)
	return m
}

// TestFinalArbiterRace races launch, engineering isolation, and cancellation
// and asserts exactly one terminal result with deterministic loser errors.
func TestFinalArbiterRace(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := toLaunchable(t, svc, validRequest())

	results := []domain.FinalResult{domain.FinalLaunch, domain.FinalIsolation, domain.FinalCancelled}
	start := make(chan struct{})
	out := make(chan struct {
		res domain.FinalOutcome
		err error
	}, len(results))
	var wg sync.WaitGroup

	for _, r := range results {
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			o, err := svc.Finalize(ctx, m.MissionID, aggregate.FinalizeRequest{
				Generation: m.Generation, OpKey: "final-" + string(r), Result: r,
			})
			out <- struct {
				res domain.FinalOutcome
				err error
			}{o, err}
		}()
	}
	close(start)
	wg.Wait()
	close(out)

	var winner domain.FinalOutcome
	successes := 0
	committed := 0
	for r := range out {
		if r.err == nil {
			successes++
			winner = r.res
			continue
		}
		var de *domain.Error
		if errors.As(r.err, &de) && de.Code == domain.ErrCodeFinalAlreadyCommitted {
			committed++
			continue
		}
		t.Fatalf("unexpected error: %v", r.err)
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1", successes)
	}
	if committed != len(results)-1 {
		t.Fatalf("committed losers = %d, want %d", committed, len(results)-1)
	}
	if !winner.State.IsTerminal() {
		t.Fatalf("winner state = %q, want terminal", winner.State)
	}
	if winner.TerminalResult == domain.FinalLaunch && winner.CredentialNonce == "" {
		t.Fatal("successful launch must carry a unique credential")
	}
}

// TestFinalizeRejectsLateMutations verifies terminal missions reject later
// ordinary mutations without state change.
func TestFinalizeRejectsLateMutations(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := toLaunchable(t, svc, validRequest())

	if _, err := svc.Finalize(ctx, m.MissionID, aggregate.FinalizeRequest{
		Generation: m.Generation, Result: domain.FinalCancelled,
	}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	_, err := svc.SimulateSegment(ctx, m.MissionID, 1, aggregate.SimulateRequest{
		Generation: m.Generation, AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	})
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeTerminal {
		t.Fatalf("code = %q, want terminal", de.Code)
	}
}
