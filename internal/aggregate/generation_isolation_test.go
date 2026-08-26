package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// TestStaleGenerationIsolation verifies that late requests for an older
// generation neither append to the current evidence chain nor change the
// current conclusion.
func TestStaleGenerationIsolation(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	// Late segment callback for an older generation is rejected without
	// appending evidence.
	_, err := svc.SimulateSegment(ctx, m.MissionID, 1, aggregate.SimulateRequest{
		Generation: 0, AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	})
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeStaleGeneration {
		t.Fatalf("code = %q, want stale_generation", de.Code)
	}

	// Late vessel callback for an older generation neither appends an attempt
	// nor confirms the vessel.
	_, err = svc.RecordVesselConfirmation(ctx, m.MissionID, aggregate.VesselRequest{
		Generation: 0, AdapterStatus: domain.AttemptSuccess, ResponseHash: "late",
	})
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeStaleGeneration {
		t.Fatalf("vessel code = %q, want stale_generation", de.Code)
	}

	detail, err := svc.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.SegmentEvidence) != 0 {
		t.Fatalf("segment evidence = %d, want 0 (stale callbacks must not append)", len(detail.SegmentEvidence))
	}
	if detail.RoutePrefix != 0 {
		t.Fatalf("route prefix = %d, want 0", detail.RoutePrefix)
	}
	if detail.VesselConfirmed {
		t.Fatal("vessel should not be confirmed by a stale callback")
	}
}

// TestEvidenceAppendOnly verifies that retry attempts accumulate as immutable
// predecessor-linked evidence without advancing the prefix.
func TestEvidenceAppendOnly(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	fail := aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictFail, ReasonCode: "sim_fault",
	}
	if _, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(fail, "try-1")); err != nil {
		t.Fatalf("fail attempt: %v", err)
	}

	pass := aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}
	if _, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(pass, "try-2")); err != nil {
		t.Fatalf("pass attempt: %v", err)
	}

	detail, err := svc.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.SegmentEvidence) != 2 {
		t.Fatalf("segment evidence = %d, want 2 (append-only)", len(detail.SegmentEvidence))
	}
	// The second attempt links to the first via its predecessor pointer.
	if detail.SegmentEvidence[1].PredecessorID != detail.SegmentEvidence[0].EvidenceID {
		t.Fatal("second attempt predecessor should point to the first attempt")
	}
	if detail.RoutePrefix != 1 {
		t.Fatalf("route prefix = %d, want 1", detail.RoutePrefix)
	}
}
