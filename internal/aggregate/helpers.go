package aggregate

import (
	"context"
	"errors"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// allReviewKinds lists the four independent review kinds that must each carry a
// passing decision before a mission is considered fully reviewed.
var allReviewKinds = []domain.ReviewKind{
	domain.ReviewRoute,
	domain.ReviewMargins,
	domain.ReviewVesselConfirmation,
	domain.ReviewSeaState,
}

// acceptedPrefix computes the contiguous accepted segment prefix: the largest k
// such that segments 1..k each have at least one pass evidence record.
func acceptedPrefix(evidence []domain.SegmentEvidence, maxSegments int64) int64 {
	passed := make(map[int64]bool, len(evidence))
	for _, ev := range evidence {
		if ev.Verdict == domain.VerdictPass {
			passed[ev.SegmentSeq] = true
		}
	}
	prefix := int64(0)
	for seq := int64(1); seq <= maxSegments; seq++ {
		if !passed[seq] {
			break
		}
		prefix = seq
	}
	return prefix
}

// vesselConfirmed reports whether any vessel adapter attempt succeeded.
func vesselConfirmed(attempts []domain.AdapterAttempt) bool {
	for _, a := range attempts {
		if a.AdapterKind == domain.AdapterVessel && a.Status == domain.AttemptSuccess {
			return true
		}
	}
	return false
}

// reviewComplete reports whether every review kind has a passing decision from
// qualified independent reviewers and at least two distinct reviewers have
// contributed a passing decision.
func reviewComplete(reviews []domain.Review) bool {
	kindPassed := make(map[domain.ReviewKind]bool)
	reviewers := make(map[string]bool)
	for _, r := range reviews {
		if r.Decision != domain.VerdictPass {
			continue
		}
		kindPassed[r.ReviewKind] = true
		reviewers[r.ReviewerID] = true
	}
	for _, kind := range allReviewKinds {
		if !kindPassed[kind] {
			return false
		}
	}
	return len(reviewers) >= 2
}

// missionTerminal returns a terminal-state error envelope for the mission.
func missionTerminal(op string, m domain.Mission) *domain.Error {
	return domain.NewError(domain.ErrCodeTerminal, op).
		WithMission(m.MissionID, m.Generation, m.State)
}

// loadMissionTx reads a mission and its waypoints inside a transaction and
// normalizes the not-found case into a stable error envelope.
func loadMissionTx(ctx context.Context, tx store.Tx, missionID, operation string) (domain.Mission, []domain.Waypoint, error) {
	m, wps, err := tx.GetMission(ctx, missionID)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Mission{}, nil, domain.NewError(domain.ErrCodeNotFound, operation).
			WithMission(missionID, 0, "")
	}
	if err != nil {
		return domain.Mission{}, nil, domain.NewError(domain.ErrCodeInternal, operation)
	}
	return m, wps, nil
}

// checkGeneration rejects requests whose generation reference is stale relative
// to the current locked generation.
func checkGeneration(m domain.Mission, generation int64, operation string) *domain.Error {
	if generation != m.Generation {
		return domain.NewError(domain.ErrCodeStaleGeneration, operation).
			WithMission(m.MissionID, m.Generation, m.State)
	}
	return nil
}

// checkNotTerminal rejects mutations on terminal missions.
func checkNotTerminal(m domain.Mission, operation string) *domain.Error {
	if m.State.IsTerminal() {
		return missionTerminal(operation, m)
	}
	return nil
}

// resolveIdempotency inspects an operation key. It returns replay=true when the
// operation was already applied with an identical body (the caller must return
// the current state), or an error for a content conflict or a replayed failure.
func (a *Aggregate) resolveIdempotency(
	ctx context.Context,
	tx store.Tx,
	opKey, operationKind, missionID string,
	generation int64,
	requestHash, operation string,
) (replay bool, err error) {
	if opKey == "" {
		return false, nil
	}
	rec, found, rerr := tx.GetIdempotency(ctx, opKey, operationKind)
	if rerr != nil {
		return false, domain.NewError(domain.ErrCodeInternal, operation)
	}
	if !found {
		return false, nil
	}
	if rec.RequestHash != requestHash {
		return false, domain.NewError(domain.ErrCodeContentConflict, operation).
			WithMission(missionID, generation, "")
	}
	if rec.StableErrorCode != "" {
		return false, domain.NewError(rec.StableErrorCode, operation).
			WithMission(missionID, generation, "")
	}
	return true, nil
}

// recordIdempotency writes a success outcome for an operation key.
func recordIdempotency(
	ctx context.Context,
	tx store.Tx,
	opKey, operationKind, missionID string,
	generation int64,
	requestHash, responseHash string,
	statusCode int,
) error {
	if opKey == "" {
		return nil
	}
	return tx.InsertIdempotency(ctx, domain.IdempotencyRecord{
		OpKey:         opKey,
		OperationKind: operationKind,
		MissionID:     missionID,
		Generation:    generation,
		RequestHash:   requestHash,
		ResponseHash:  responseHash,
		StatusCode:    statusCode,
		CreatedAtTick: 0,
	})
}
