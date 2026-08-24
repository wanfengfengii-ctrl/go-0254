package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// authorizeTwo signs the locked generation with the two distinct technical
// authorizers (a technical_authorizer and a safety_authorizer).
func authorizeTwo(t *testing.T, svc *aggregate.Aggregate, m domain.Mission) {
	t.Helper()
	_, err := svc.Authorize(context.Background(), m.MissionID, aggregate.AuthorizeRequest{
		Generation:  m.Generation,
		OpKey:       "auth-" + m.MissionID + "-1",
		SignerID:    "eng-alice",
		Role:        domain.RoleTechnicalAuthorizer,
		PayloadHash: m.LockedPayloadHash,
	})
	if err != nil {
		t.Fatalf("authorize technical: %v", err)
	}
	_, err = svc.Authorize(context.Background(), m.MissionID, aggregate.AuthorizeRequest{
		Generation:  m.Generation,
		OpKey:       "auth-" + m.MissionID + "-2",
		SignerID:    "eng-bob",
		Role:        domain.RoleSafetyAuthorizer,
		PayloadHash: m.LockedPayloadHash,
	})
	if err != nil {
		t.Fatalf("authorize safety: %v", err)
	}
}

// acquireLeases captures the three exclusive resources.
func acquireLeases(t *testing.T, svc *aggregate.Aggregate, m domain.Mission) aggregate.LeaseResult {
	t.Helper()
	res, err := svc.AcquireLeases(context.Background(), m.MissionID, aggregate.LeaseRequest{
		Generation: m.Generation,
		OpKey:      "lease-" + m.MissionID,
	})
	if err != nil {
		t.Fatalf("acquire leases: %v", err)
	}
	return res
}

// simulateAll passes every segment in sequence, driving the prefix to complete.
func simulateAll(t *testing.T, svc *aggregate.Aggregate, m domain.Mission, n int) {
	t.Helper()
	for seq := int64(1); seq <= int64(n); seq++ {
		_, err := svc.SimulateSegment(context.Background(), m.MissionID, seq, aggregate.SimulateRequest{
			Generation:         m.Generation,
			OpKey:              "seg-" + itoa(seq),
			SimulatorScriptRef: "sim-script-01",
			AdapterStatus:      domain.AttemptSuccess,
			Verdict:            domain.VerdictPass,
		})
		if err != nil {
			t.Fatalf("simulate segment %d: %v", seq, err)
		}
	}
}

// reviewAll records passing decisions for all four kinds from two distinct
// qualified independent reviewers.
func reviewAll(t *testing.T, svc *aggregate.Aggregate, m domain.Mission) {
	t.Helper()
	reviewers := []string{"eng-alice", "eng-alice", "sea-carol", "sea-carol"}
	kinds := []domain.ReviewKind{
		domain.ReviewRoute, domain.ReviewMargins,
		domain.ReviewVesselConfirmation, domain.ReviewSeaState,
	}
	for i, kind := range kinds {
		_, err := svc.RecordReview(context.Background(), m.MissionID, aggregate.ReviewRequest{
			Generation:   m.Generation,
			OpKey:        "review-" + itoa(int64(i+1)),
			ReviewerID:   reviewers[i],
			ReviewKind:   kind,
			Decision:     domain.VerdictPass,
			EvidenceHash: "evidence-" + itoa(int64(i+1)),
		})
		if err != nil {
			t.Fatalf("review %s: %v", kind, err)
		}
	}
}

// TestFullWorkflow drives a deep-sea mission from lock through a successful
// launch, exercising every state transition and the final credential.
func TestFullWorkflow(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	m, err := svc.LockMission(ctx, validRequest())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if m.State != domain.StatePendingAuthorization {
		t.Fatalf("state after lock = %q", m.State)
	}

	authorizeTwo(t, svc, m)

	lr := acquireLeases(t, svc, m)
	if lr.State != domain.StateLeasesHeld {
		t.Fatalf("state after leases = %q, want leases_held", lr.State)
	}
	if len(lr.Leases) != 3 {
		t.Fatalf("leases = %d, want 3", len(lr.Leases))
	}

	simulateAll(t, svc, m, 1)

	mr, err := svc.CheckMargins(ctx, m.MissionID, aggregate.MarginRequest{Generation: m.Generation, OpKey: "margin-1"})
	if err != nil {
		t.Fatalf("margins: %v", err)
	}
	if !mr.Passed {
		t.Fatalf("margins did not pass: %v", mr.Reasons)
	}
	if mr.State != domain.StatePendingVesselConfirmation {
		t.Fatalf("state after margins = %q, want pending_vessel_confirmation", mr.State)
	}

	vr, err := svc.RecordVesselConfirmation(ctx, m.MissionID, aggregate.VesselRequest{
		Generation:    m.Generation,
		OpKey:         "vessel-1",
		AdapterStatus: domain.AttemptSuccess,
		ResponseHash:  "vessel-reply-1",
	})
	if err != nil {
		t.Fatalf("vessel confirm: %v", err)
	}
	if !vr.Confirmed {
		t.Fatal("vessel should be confirmed")
	}

	reviewAll(t, svc, m)

	detail, err := svc.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.State != domain.StateLaunchable {
		t.Fatalf("state after reviews = %q, want launchable", detail.State)
	}
	if !detail.ReviewComplete {
		t.Fatal("reviews should be complete")
	}

	outcome, err := svc.Finalize(ctx, m.MissionID, aggregate.FinalizeRequest{
		Generation: m.Generation,
		OpKey:      "finalize-1",
		Result:     domain.FinalLaunch,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if outcome.State != domain.StateLaunched {
		t.Fatalf("final state = %q, want launched", outcome.State)
	}
	if outcome.CredentialNonce == "" {
		t.Fatal("launch credential should be unique and non-empty")
	}
}

// TestFinalizeLaunchRequiresPrerequisites verifies the arbiter refuses launch
// before the full prerequisite chain is satisfied.
func TestFinalizeLaunchRequiresPrerequisites(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	m, err := svc.LockMission(ctx, validRequest())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}

	_, err = svc.Finalize(ctx, m.MissionID, aggregate.FinalizeRequest{
		Generation: m.Generation,
		Result:     domain.FinalLaunch,
	})
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodePrerequisiteMissing {
		t.Fatalf("code = %q, want prerequisite_missing", de.Code)
	}
}
