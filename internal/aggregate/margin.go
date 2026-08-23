package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// MarginRequest runs the deterministic return-safety arithmetic for the route.
type MarginRequest struct {
	Generation int64  `json:"generation"`
	OpKey      string `json:"op_key"`
}

// MarginResult reports the outcome of a margin check.
type MarginResult struct {
	MissionID   string            `json:"mission_id"`
	Generation  int64             `json:"generation"`
	State       domain.State      `json:"state"`
	Passed      bool              `json:"passed"`
	Computation MarginComputation `json:"computation"`
	Reasons     []string          `json:"reasons,omitempty"`
}

// marginBody is the canonical serialization unit for margin idempotency.
type marginBody struct {
	Generation int64 `json:"generation"`
}

// CheckMargins runs deterministic energy, depth, speed, return-reserve, and
// buoyancy arithmetic with explicit integer bounds and overflow checks. Only a
// fully passing check advances the mission toward vessel confirmation.
func (a *Aggregate) CheckMargins(ctx context.Context, missionID string, req MarginRequest) (MarginResult, error) {
	operation := "margins_check"

	requestHash, err := domain.HashJSON(marginBody{Generation: req.Generation})
	if err != nil {
		return MarginResult{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var result MarginResult
	err = a.store.WithTx(ctx, func(tx store.Tx) error {
		m, wps, lerr := loadMissionTx(ctx, tx, missionID, operation)
		if lerr != nil {
			return lerr
		}
		if terr := checkNotTerminal(m, operation); terr != nil {
			return terr
		}
		if terr := checkGeneration(m, req.Generation, operation); terr != nil {
			return terr
		}
		if m.State != domain.StateMarginChecking {
			return domain.NewError(domain.ErrCodeStateMachine, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("route_prefix_incomplete")
		}

		hull, ok := a.cat.Hull(m.AUVHullID)
		if !ok {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		comp, cerr := computeMargins(m, wps, hull)
		if cerr != nil {
			return cerr
		}
		reasons := evaluateMargins(m, comp, hull)
		passed := len(reasons) == 0

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			result = MarginResult{
				MissionID:   m.MissionID,
				Generation:  m.Generation,
				State:       m.State,
				Passed:      passed,
				Computation: comp,
				Reasons:     reasons,
			}
			return nil
		}

		tick := a.now()
		verdict := domain.VerdictPass
		reasonCode := ""
		if !passed {
			verdict = domain.VerdictFail
			reasonCode = reasons[0]
		}
		evidence := domain.MarginEvidence{
			EvidenceID:      a.newID(),
			MissionID:       missionID,
			Generation:      m.Generation,
			RoutePrefix:     int64(len(wps)) - 1,
			EnergyUsedWh:    comp.EnergyUsedWh,
			ReturnReserveWh: comp.ReturnReserveWh,
			PeakDepthM:      comp.PeakDepthM,
			PeakSpeedCmS:    comp.PeakSpeedCmS,
			TrimDeltaG:      comp.TrimDeltaG,
			Verdict:         verdict,
			ReasonCode:      reasonCode,
			CreatedAtTick:   tick,
		}
		if ierr := tx.InsertMarginEvidence(ctx, evidence); ierr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		if passed {
			m.State = domain.StatePendingVesselConfirmation
			m.UpdatedAtTick = tick
			if uerr := tx.UpdateMission(ctx, m, m.AggregateVersion); uerr != nil {
				if uerr == store.ErrVersionConflict {
					return domain.NewError(domain.ErrCodeFinalAlreadyCommitted, operation).
						WithMission(m.MissionID, m.Generation, m.State)
				}
				return domain.NewError(domain.ErrCodeInternal, operation)
			}
		}

		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 200); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		result = MarginResult{
			MissionID:   m.MissionID,
			Generation:  m.Generation,
			State:       m.State,
			Passed:      passed,
			Computation: comp,
			Reasons:     reasons,
		}
		return nil
	})
	if err != nil {
		return MarginResult{}, err
	}
	return result, nil
}
