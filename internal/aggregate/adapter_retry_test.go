package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// toVesselConfirmation drives a mission to pending_vessel_confirmation.
func toVesselConfirmation(t *testing.T, svc *aggregate.Aggregate) domain.Mission {
	t.Helper()
	m := lockToMargins(t, svc, validRequest())
	if _, err := svc.CheckMargins(context.Background(), m.MissionID, aggregate.MarginRequest{Generation: m.Generation}); err != nil {
		t.Fatalf("margins: %v", err)
	}
	return m
}

// TestVesselRetryAttempts scripts refusal, disconnect, timeout, and malformed
// vessel replies and asserts retry metadata without any false confirmation.
func TestVesselRetryAttempts(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := toVesselConfirmation(t, svc)

	statuses := []domain.AttemptStatus{
		domain.AttemptRefused, domain.AttemptDisconnect, domain.AttemptTimeout, domain.AttemptMalformed,
	}
	for i, status := range statuses {
		res, err := svc.RecordVesselConfirmation(ctx, m.MissionID, aggregate.VesselRequest{
			Generation: m.Generation, OpKey: "vessel-" + itoa(int64(i+1)), AdapterStatus: status, ReasonCode: "reason-" + string(status),
		})
		if err != nil {
			t.Fatalf("vessel %s: %v", status, err)
		}
		if res.Confirmed {
			t.Fatalf("status %s must not confirm the vessel", status)
		}
		if res.RetryAfterTick <= 0 {
			t.Fatalf("status %s should schedule a retry", status)
		}
		if res.AttemptNo != int64(i+1) {
			t.Fatalf("attempt no = %d, want %d", res.AttemptNo, i+1)
		}
	}

	// A genuine success closes the confirmation.
	ok, err := svc.RecordVesselConfirmation(ctx, m.MissionID, aggregate.VesselRequest{
		Generation: m.Generation, OpKey: "vessel-ok", AdapterStatus: domain.AttemptSuccess, ResponseHash: "reply-1",
	})
	if err != nil {
		t.Fatalf("success: %v", err)
	}
	if !ok.Confirmed {
		t.Fatal("success should confirm the vessel")
	}

	detail, err := svc.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.AdapterAttempts) != 5 {
		t.Fatalf("adapter attempts = %d, want 5", len(detail.AdapterAttempts))
	}
}

// TestContradictoryConfirmation verifies a second success with different
// content is rejected deterministically.
func TestContradictoryConfirmation(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := toVesselConfirmation(t, svc)

	if _, err := svc.RecordVesselConfirmation(ctx, m.MissionID, aggregate.VesselRequest{
		Generation: m.Generation, OpKey: "v-1", AdapterStatus: domain.AttemptSuccess, ResponseHash: "reply-A",
	}); err != nil {
		t.Fatalf("first success: %v", err)
	}

	_, err := svc.RecordVesselConfirmation(ctx, m.MissionID, aggregate.VesselRequest{
		Generation: m.Generation, OpKey: "v-2", AdapterStatus: domain.AttemptSuccess, ResponseHash: "reply-B",
	})
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeVesselConflict {
		t.Fatalf("code = %q, want contradictory_confirmation", de.Code)
	}
}

// TestSimulatorRetryRecords verifies a refused simulator call is audited as a
// retry record without advancing the prefix.
func TestSimulatorRetryRecords(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	m := lockedAndLeased(t, svc, threeSegmentRequest())

	res, err := svc.SimulateSegment(ctx, m.MissionID, 1, aggregate.SimulateRequest{
		Generation: m.Generation, OpKey: "sim-refused", SimulatorScriptRef: "s",
		AdapterStatus: domain.AttemptRefused,
	})
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if res.Verdict != domain.VerdictRetry {
		t.Fatalf("verdict = %q, want retry", res.Verdict)
	}
	if res.PrefixAfter != 0 {
		t.Fatalf("refused call advanced prefix to %d", res.PrefixAfter)
	}

	detail, err := svc.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.SegmentEvidence) != 1 {
		t.Fatalf("segment evidence = %d, want 1", len(detail.SegmentEvidence))
	}
}
