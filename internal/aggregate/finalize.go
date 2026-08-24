package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// FinalizeRequest commits a terminal result through the final arbiter.
type FinalizeRequest struct {
	Generation int64              `json:"generation"`
	OpKey      string             `json:"op_key"`
	Result     domain.FinalResult `json:"result"`
}

// finalizeBody is the canonical serialization unit for finalize idempotency.
type finalizeBody struct {
	Generation int64              `json:"generation"`
	Result     domain.FinalResult `json:"result"`
}

// Finalize commits launch, engineering isolation, or cancellation through a
// single compare-and-set guarded by the aggregate version. Only a successful
// launch produces the unique credential nonce.
func (a *Aggregate) Finalize(ctx context.Context, missionID string, req FinalizeRequest) (domain.FinalOutcome, error) {
	operation := "finalize"

	if !validFinalResult(req.Result) {
		return domain.FinalOutcome{}, domain.NewError(domain.ErrCodeUnknownResult, operation).
			WithReasons("unknown_final_result")
	}

	requestHash, err := domain.HashJSON(finalizeBody{Generation: req.Generation, Result: req.Result})
	if err != nil {
		return domain.FinalOutcome{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var outcome domain.FinalOutcome
	err = a.store.WithTx(ctx, func(tx store.Tx) error {
		m, wps, lerr := loadMissionTx(ctx, tx, missionID, operation)
		if lerr != nil {
			return lerr
		}
		if terr := checkGeneration(m, req.Generation, operation); terr != nil {
			return terr
		}

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			return a.buildOutcome(ctx, tx, m, &outcome)
		}

		// A previous finalize committed: report the committed result.
		if m.State.IsTerminal() {
			return a.finalAlreadyCommitted(ctx, tx, m, operation)
		}

		// Launch requires the full prerequisite chain; isolation and
		// cancellation are available from any non-terminal state.
		if req.Result == domain.FinalLaunch {
			if reasons := a.launchPrerequisites(ctx, tx, m, wps); len(reasons) > 0 {
				return domain.NewError(domain.ErrCodePrerequisiteMissing, operation).
					WithMission(m.MissionID, m.Generation, m.State).
					WithReasons(domain.SortedReasons(reasons...)...)
			}
		}

		tick := a.now()
		terminalState := stateForResult(req.Result)

		m.State = terminalState
		m.TerminalResult = string(req.Result)
		m.UpdatedAtTick = tick
		if uerr := tx.UpdateMission(ctx, m, m.AggregateVersion); uerr != nil {
			if uerr == store.ErrVersionConflict {
				return a.finalAlreadyCommitted(ctx, tx, m, operation)
			}
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		// Leases are released only on terminal outcomes.
		if rerr := tx.ReleaseLeases(ctx, missionID, tick); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		nonce := ""
		if req.Result == domain.FinalLaunch {
			nonce = a.newID()
			if ierr := tx.InsertFinal(ctx, missionID, m.Generation, req.Result, nonce, tick); ierr != nil {
				return domain.NewError(domain.ErrCodeInternal, operation)
			}
		}

		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 200); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		outcome = domain.FinalOutcome{
			MissionID:       m.MissionID,
			Generation:      m.Generation,
			State:           terminalState,
			TerminalResult:  req.Result,
			CredentialNonce: nonce,
		}
		return nil
	})
	if err != nil {
		return domain.FinalOutcome{}, err
	}
	return outcome, nil
}

// buildOutcome reconstructs the committed outcome for an idempotent replay.
func (a *Aggregate) buildOutcome(ctx context.Context, tx store.Tx, m domain.Mission, outcome *domain.FinalOutcome) error {
	if m.State.IsTerminal() {
		final, found, err := tx.GetFinal(ctx, m.MissionID, m.Generation)
		if err != nil {
			return domain.NewError(domain.ErrCodeInternal, "finalize")
		}
		if found {
			*outcome = final
			return nil
		}
	}
	*outcome = domain.FinalOutcome{
		MissionID:      m.MissionID,
		Generation:     m.Generation,
		State:          m.State,
		TerminalResult: domain.FinalResult(m.TerminalResult),
	}
	return nil
}

// finalAlreadyCommitted builds the deterministic loser envelope carrying the
// committed terminal result.
func (a *Aggregate) finalAlreadyCommitted(ctx context.Context, tx store.Tx, m domain.Mission, operation string) *domain.Error {
	result := m.TerminalResult
	if result == "" {
		result = string(m.State)
	}
	return domain.NewError(domain.ErrCodeFinalAlreadyCommitted, operation).
		WithMission(m.MissionID, m.Generation, m.State).
		WithReasons("committed:" + result)
}

// launchPrerequisites returns the missing prerequisite reason codes for a
// successful launch, or nil when the chain is complete.
func (a *Aggregate) launchPrerequisites(ctx context.Context, tx store.Tx, m domain.Mission, wps []domain.Waypoint) []string {
	var reasons []string
	maxSegments := int64(len(wps)) - 1

	segments, err := tx.GetSegmentEvidence(ctx, m.MissionID, m.Generation)
	if err != nil {
		return []string{"evidence_read_failure"}
	}
	if acceptedPrefix(segments, maxSegments) < maxSegments {
		reasons = append(reasons, "route_prefix_incomplete")
	}

	margins, err := tx.GetMarginEvidence(ctx, m.MissionID, m.Generation)
	if err != nil {
		return []string{"evidence_read_failure"}
	}
	if !latestMarginPass(margins) {
		reasons = append(reasons, "margins_not_passed")
	}

	attempts, err := tx.GetAdapterAttempts(ctx, m.MissionID, m.Generation)
	if err != nil {
		return []string{"evidence_read_failure"}
	}
	if !vesselConfirmed(attempts) {
		reasons = append(reasons, "vessel_not_confirmed")
	}

	reviews, err := tx.GetReviews(ctx, m.MissionID, m.Generation)
	if err != nil {
		return []string{"evidence_read_failure"}
	}
	if !reviewComplete(reviews) {
		reasons = append(reasons, "review_incomplete")
	}
	return reasons
}

// latestMarginPass reports whether the most recent margin evidence passed.
func latestMarginPass(margins []domain.MarginEvidence) bool {
	if len(margins) == 0 {
		return false
	}
	return margins[len(margins)-1].Verdict == domain.VerdictPass
}

// validFinalResult reports whether r is a documented terminal result.
func validFinalResult(r domain.FinalResult) bool {
	switch r {
	case domain.FinalLaunch, domain.FinalIsolation, domain.FinalCancelled:
		return true
	default:
		return false
	}
}

// stateForResult maps a final result to its terminal mission state.
func stateForResult(r domain.FinalResult) domain.State {
	switch r {
	case domain.FinalLaunch:
		return domain.StateLaunched
	case domain.FinalIsolation:
		return domain.StateEngineeringIsolation
	case domain.FinalCancelled:
		return domain.StateCancelled
	default:
		return ""
	}
}
