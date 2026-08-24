package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

func TestModel_SegmentSimulationRetryAndFailIdempotency(t *testing.T) {
	cases := []struct {
		name        string
		first       aggregate.SimulateRequest
		conflicting aggregate.SimulateRequest
		wantStatus  domain.AttemptStatus
		wantVerdict domain.Verdict
	}{
		{
			name: "refused retry evidence",
			first: aggregate.SimulateRequest{
				SimulatorScriptRef: "sim-route-1",
				AdapterStatus:      domain.AttemptRefused,
			},
			conflicting: aggregate.SimulateRequest{
				SimulatorScriptRef: "sim-route-1",
				AdapterStatus:      domain.AttemptRefused,
				ReasonCode:         "different-refusal-body",
			},
			wantStatus:  domain.AttemptRefused,
			wantVerdict: domain.VerdictRetry,
		},
		{
			name: "simulator fail evidence",
			first: aggregate.SimulateRequest{
				SimulatorScriptRef: "sim-route-1",
				AdapterStatus:      domain.AttemptSuccess,
				Verdict:            domain.VerdictFail,
				ReasonCode:         "return_margin_negative",
				ResponseHash:       "sim-fail-response",
			},
			conflicting: aggregate.SimulateRequest{
				SimulatorScriptRef: "sim-route-1",
				AdapterStatus:      domain.AttemptSuccess,
				Verdict:            domain.VerdictFail,
				ReasonCode:         "trim_outside_bounds",
				ResponseHash:       "sim-fail-response",
			},
			wantStatus:  domain.AttemptSuccess,
			wantVerdict: domain.VerdictFail,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newService(t)
			ctx := context.Background()
			m := lockedAndLeased(t, svc, threeSegmentRequest())
			opKey := "segment-1-idempotent"

			first := tc.first
			first.Generation = m.Generation
			first.OpKey = opKey
			got, err := svc.SimulateSegment(ctx, m.MissionID, 1, first)
			if err != nil {
				t.Fatalf("first simulate: %v", err)
			}
			if got.AttemptNo != 1 {
				t.Fatalf("first attempt_no = %d, want 1", got.AttemptNo)
			}
			if got.PrefixBefore != 0 || got.PrefixAfter != 0 {
				t.Fatalf("first simulate changed prefix: before=%d after=%d", got.PrefixBefore, got.PrefixAfter)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("first status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.Verdict != tc.wantVerdict {
				t.Fatalf("first verdict = %q, want %q", got.Verdict, tc.wantVerdict)
			}

			detail, err := svc.GetMission(ctx, m.MissionID)
			if err != nil {
				t.Fatalf("get after first simulate: %v", err)
			}
			if len(detail.SegmentEvidence) != 1 {
				t.Fatalf("segment evidence after first simulate = %d, want 1", len(detail.SegmentEvidence))
			}
			if detail.SegmentEvidence[0].AttemptNo != 1 {
				t.Fatalf("persisted first attempt_no = %d, want 1", detail.SegmentEvidence[0].AttemptNo)
			}

			replay, err := svc.SimulateSegment(ctx, m.MissionID, 1, first)
			if err != nil {
				t.Fatalf("idempotent replay: %v", err)
			}
			if !replay.Idempotent {
				t.Fatal("same op_key and body should replay idempotently")
			}

			detail, err = svc.GetMission(ctx, m.MissionID)
			if err != nil {
				t.Fatalf("get after replay: %v", err)
			}
			if len(detail.SegmentEvidence) != 1 {
				t.Fatalf("segment evidence after replay = %d, want 1", len(detail.SegmentEvidence))
			}
			if detail.SegmentEvidence[0].AttemptNo != 1 {
				t.Fatalf("persisted replay attempt_no = %d, want 1", detail.SegmentEvidence[0].AttemptNo)
			}

			conflicting := tc.conflicting
			conflicting.Generation = m.Generation
			conflicting.OpKey = opKey
			_, err = svc.SimulateSegment(ctx, m.MissionID, 1, conflicting)
			var de *domain.Error
			if !errors.As(err, &de) {
				t.Fatalf("expected content conflict, got %v", err)
			}
			if de.Code != domain.ErrCodeContentConflict {
				t.Fatalf("conflict code = %q, want %q", de.Code, domain.ErrCodeContentConflict)
			}

			detail, err = svc.GetMission(ctx, m.MissionID)
			if err != nil {
				t.Fatalf("get after conflict: %v", err)
			}
			if len(detail.SegmentEvidence) != 1 {
				t.Fatalf("segment evidence after conflict = %d, want 1", len(detail.SegmentEvidence))
			}
		})
	}
}
