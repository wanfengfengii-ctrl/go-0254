package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// VesselRequest records a vessel adapter confirmation attempt.
type VesselRequest struct {
	Generation    int64                `json:"generation"`
	OpKey         string               `json:"op_key"`
	AdapterStatus domain.AttemptStatus `json:"adapter_status"`
	ReasonCode    string               `json:"reason_code,omitempty"`
	ResponseHash  string               `json:"response_hash,omitempty"`
}

// VesselResult reports the vessel confirmation ledger after an attempt.
type VesselResult struct {
	MissionID      string       `json:"mission_id"`
	Generation     int64        `json:"generation"`
	State          domain.State `json:"state"`
	Confirmed      bool         `json:"confirmed"`
	AttemptNo      int64        `json:"attempt_no"`
	RetryAfterTick int64        `json:"retry_after_tick,omitempty"`
}

// vesselBody is the canonical serialization unit for vessel idempotency.
type vesselBody struct {
	Generation    int64                `json:"generation"`
	AdapterStatus domain.AttemptStatus `json:"adapter_status"`
	ReasonCode    string               `json:"reason_code,omitempty"`
	ResponseHash  string               `json:"response_hash,omitempty"`
}

// retryBackoff returns the deterministic retry interval in logical ticks for a
// failed adapter status.
func retryBackoff(status domain.AttemptStatus) int64 {
	switch status {
	case domain.AttemptRefused:
		return 10
	case domain.AttemptDisconnect:
		return 20
	case domain.AttemptTimeout:
		return 30
	case domain.AttemptMalformed:
		return 40
	default:
		return 0
	}
}

// RecordVesselConfirmation records a vessel adapter attempt without fabricating
// success. Refusals, disconnects, timeouts, and malformed replies create
// retryable audit records only; contradictory confirmations are rejected.
func (a *Aggregate) RecordVesselConfirmation(ctx context.Context, missionID string, req VesselRequest) (VesselResult, error) {
	operation := "vessel_confirmation"

	body := vesselBody{Generation: req.Generation, AdapterStatus: req.AdapterStatus, ReasonCode: req.ReasonCode, ResponseHash: req.ResponseHash}
	requestHash, err := domain.HashJSON(body)
	if err != nil {
		return VesselResult{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var result VesselResult
	err = a.store.WithTx(ctx, func(tx store.Tx) error {
		m, _, lerr := loadMissionTx(ctx, tx, missionID, operation)
		if lerr != nil {
			return lerr
		}
		if terr := checkNotTerminal(m, operation); terr != nil {
			return terr
		}
		if terr := checkGeneration(m, req.Generation, operation); terr != nil {
			return terr
		}
		if m.State != domain.StatePendingVesselConfirmation && m.State != domain.StateLaunchable {
			return domain.NewError(domain.ErrCodeStateMachine, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("vessel_confirmation_not_open")
		}

		attempts, gerr := tx.GetAdapterAttempts(ctx, missionID, m.Generation)
		if gerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		vesselAttempts := filterVessel(attempts)
		attemptNo := int64(len(vesselAttempts)) + 1
		alreadyConfirmed, confirmedHash := latestVesselSuccess(vesselAttempts)

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			result = VesselResult{
				MissionID:  m.MissionID,
				Generation: m.Generation,
				State:      m.State,
				Confirmed:  alreadyConfirmed,
				AttemptNo:  attemptNo - 1,
			}
			return nil
		}

		tick := a.now()
		attempt := domain.AdapterAttempt{
			AttemptID:     a.newID(),
			MissionID:     missionID,
			Generation:    m.Generation,
			AdapterKind:   domain.AdapterVessel,
			RequestHash:   requestHash,
			Status:        req.AdapterStatus,
			ReasonCode:    req.ReasonCode,
			AttemptNo:     attemptNo,
			CreatedAtTick: tick,
		}
		if req.AdapterStatus != domain.AttemptSuccess {
			attempt.RetryAfterTick = tick + retryBackoff(req.AdapterStatus)
		}
		if ierr := tx.InsertAdapterAttempt(ctx, attempt); ierr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		// Contradictory confirmation: a second success with different content
		// is rejected deterministically without changing the confirmed hash.
		if alreadyConfirmed && req.AdapterStatus == domain.AttemptSuccess && requestHash != confirmedHash {
			return domain.NewError(domain.ErrCodeVesselConflict, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("contradictory_confirmation")
		}

		confirmed := alreadyConfirmed || req.AdapterStatus == domain.AttemptSuccess

		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 200); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		result = VesselResult{
			MissionID:      m.MissionID,
			Generation:     m.Generation,
			State:          m.State,
			Confirmed:      confirmed,
			AttemptNo:      attemptNo,
			RetryAfterTick: attempt.RetryAfterTick,
		}
		return nil
	})
	if err != nil {
		return VesselResult{}, err
	}
	return result, nil
}

// filterVessel keeps only vessel-kind adapter attempts.
func filterVessel(attempts []domain.AdapterAttempt) []domain.AdapterAttempt {
	var out []domain.AdapterAttempt
	for _, a := range attempts {
		if a.AdapterKind == domain.AdapterVessel {
			out = append(out, a)
		}
	}
	return out
}

// latestVesselSuccess returns whether a vessel success exists and the request
// hash of the most recent success.
func latestVesselSuccess(attempts []domain.AdapterAttempt) (bool, string) {
	var latest *domain.AdapterAttempt
	for i := range attempts {
		if attempts[i].Status == domain.AttemptSuccess {
			latest = &attempts[i]
		}
	}
	if latest == nil {
		return false, ""
	}
	return true, latest.RequestHash
}
