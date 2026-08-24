package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// threeSegmentRequest builds a lock request with three waypoints (two segments).
func threeSegmentRequest() aggregate.LockRequest {
	return aggregate.LockRequest{
		VoyageID:          "VGR-01",
		AUVHullID:         "HULL-01",
		BeaconSlotID:      "BEACON-01",
		SeaStateRevision:  1,
		ReturnThresholdWh: 10000,
		DepthLimitM:       4000,
		TrimMinG:          -400,
		TrimMaxG:          400,
		Waypoints: []aggregate.WaypointInput{
			{Seq: 1, TargetDepthM: 3000, MaxSpeedCmS: 120, ExpectedDrawWh: 4000, DwellSeconds: 60},
			{Seq: 2, TargetDepthM: 3200, MaxSpeedCmS: 120, ExpectedDrawWh: 4200, DwellSeconds: 60},
			{Seq: 3, TargetDepthM: 3500, MaxSpeedCmS: 120, ExpectedDrawWh: 5000, DwellSeconds: 60},
		},
	}
}

// lockedAndLeased locks, authorizes, and captures leases, returning the mission.
func lockedAndLeased(t *testing.T, svc *aggregate.Aggregate, req aggregate.LockRequest) domain.Mission {
	t.Helper()
	m, err := svc.LockMission(context.Background(), req)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	authorizeTwo(t, svc, m)
	acquireLeases(t, svc, m)
	return m
}

func TestSegmentPrefixOrdering(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	pass := aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}

	// Accept segments in order.
	r1, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(pass, "s1"))
	if err != nil {
		t.Fatalf("seg1: %v", err)
	}
	if r1.PrefixAfter != 1 {
		t.Fatalf("prefix after seg1 = %d, want 1", r1.PrefixAfter)
	}

	// Skipped sequence: segment 3 before segment 2 leaves the prefix unchanged.
	rSkip, err := svc.SimulateSegment(ctx, m.MissionID, 3, withOp(pass, "skip"))
	if err != nil {
		t.Fatalf("skip: %v", err)
	}
	if rSkip.PrefixBefore != 1 || rSkip.PrefixAfter != 1 {
		t.Fatalf("skipped segment advanced prefix: before=%d after=%d", rSkip.PrefixBefore, rSkip.PrefixAfter)
	}
	if rSkip.Verdict != domain.VerdictRetry {
		t.Fatalf("skipped verdict = %q, want retry", rSkip.Verdict)
	}

	// Accept segment 2 to complete the prefix.
	if _, err := svc.SimulateSegment(ctx, m.MissionID, 2, withOp(pass, "s2")); err != nil {
		t.Fatalf("seg2: %v", err)
	}
}

func TestSegmentDuplicateDoesNotAdvance(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	pass := aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}
	if _, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(pass, "a")); err != nil {
		t.Fatalf("seg1: %v", err)
	}
	dup, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(pass, "dup"))
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	if dup.PrefixAfter != 1 {
		t.Fatalf("duplicate advanced prefix to %d", dup.PrefixAfter)
	}
	if dup.Verdict != domain.VerdictRetry {
		t.Fatalf("duplicate verdict = %q, want retry", dup.Verdict)
	}
}

func TestSegmentStaleGeneration(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	req := aggregate.SimulateRequest{
		Generation: 99, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}
	_, err := svc.SimulateSegment(ctx, m.MissionID, 1, req)
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeStaleGeneration {
		t.Fatalf("code = %q, want stale_generation", de.Code)
	}
}

func TestSegmentIdempotentReplayAndConflict(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	pass := aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}
	r1, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(pass, "idem"))
	if err != nil {
		t.Fatalf("seg1: %v", err)
	}

	// Identical body + same op_key replays without advancing again.
	replay, err := svc.SimulateSegment(ctx, m.MissionID, 1, withOp(pass, "idem"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Idempotent {
		t.Fatal("replay should be idempotent")
	}
	if replay.PrefixAfter != r1.PrefixAfter {
		t.Fatalf("replay changed prefix: %d -> %d", r1.PrefixAfter, replay.PrefixAfter)
	}

	// Same op_key with different content is rejected.
	conflict := aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "other", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}
	_, err = svc.SimulateSegment(ctx, m.MissionID, 1, withOp(conflict, "idem"))
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeContentConflict {
		t.Fatalf("code = %q, want content_conflict", de.Code)
	}
}

func withOp(req aggregate.SimulateRequest, opKey string) aggregate.SimulateRequest {
	req.OpKey = opKey
	return req
}
