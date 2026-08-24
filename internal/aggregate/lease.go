package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// LeaseRequest requests the atomic capture of the three exclusive resources.
type LeaseRequest struct {
	Generation int64  `json:"generation"`
	OpKey      string `json:"op_key"`
}

// LeaseResult reports the lease ledger after a successful capture.
type LeaseResult struct {
	MissionID  string         `json:"mission_id"`
	Generation int64          `json:"generation"`
	State      domain.State   `json:"state"`
	Leases     []domain.Lease `json:"leases"`
}

// leaseBody is the canonical serialization unit for lease idempotency.
type leaseBody struct {
	Generation int64 `json:"generation"`
}

// AcquireLeases atomically reserves the AUV hull, acoustic beacon slot, and
// route-package generation for the authorized mission. Competing attempts
// receive deterministic denial reasons sorted by resource key.
func (a *Aggregate) AcquireLeases(ctx context.Context, missionID string, req LeaseRequest) (LeaseResult, error) {
	operation := "acquire_leases"

	requestHash, err := domain.HashJSON(leaseBody{Generation: req.Generation})
	if err != nil {
		return LeaseResult{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var result LeaseResult
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
		if m.State != domain.StatePendingAuthorization {
			return domain.NewError(domain.ErrCodeStateMachine, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("leases_already_captured")
		}

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			return a.buildLeaseResult(ctx, tx, m, &result)
		}

		auths, gerr := tx.GetAuthorizations(ctx, missionID, m.Generation)
		if gerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		filled := make(map[domain.SignerRole]bool)
		for _, au := range auths {
			filled[au.Role] = true
		}
		for _, role := range domain.TechnicalAuthorizationRoles() {
			if !filled[role] {
				return domain.NewError(domain.ErrCodeNotAuthorized, operation).
					WithMission(m.MissionID, m.Generation, m.State).
					WithReasons("missing_" + string(role))
			}
		}

		leases := []domain.Lease{
			{ResourceType: domain.ResourceAUVHull, ResourceKey: m.AUVHullID},
			{ResourceType: domain.ResourceBeaconSlot, ResourceKey: m.BeaconSlotID},
			{ResourceType: domain.ResourceRouteGeneration, ResourceKey: routeGenerationKey(m)},
		}

		// Conflict detection before insertion for deterministic, sorted reasons.
		var conflicts []domain.Lease
		for _, l := range leases {
			open, ferr := tx.FindOpenLeases(ctx, string(l.ResourceType), l.ResourceKey)
			if ferr != nil {
				return domain.NewError(domain.ErrCodeInternal, operation)
			}
			conflicts = append(conflicts, open...)
		}
		if len(conflicts) > 0 {
			reasons := make([]string, 0, len(conflicts))
			for _, c := range conflicts {
				reasons = append(reasons, string(c.ResourceType)+":"+c.ResourceKey)
			}
			return domain.NewError(domain.ErrCodeLeaseConflict, operation).
				WithMission(m.MissionID, m.Generation, m.State).
				WithReasons(domain.SortedReasons(reasons...)...)
		}

		tick := a.now()
		for i := range leases {
			leases[i].LeaseID = a.newID()
			leases[i].MissionID = missionID
			leases[i].Generation = m.Generation
			leases[i].Status = domain.LeaseOpen
			leases[i].AcquiredAtTick = tick
		}
		if ierr := tx.InsertLeases(ctx, leases); ierr != nil {
			// The effective-open unique index guarantees no partial rows; a
			// constraint failure is a deterministic lease conflict.
			return domain.NewError(domain.ErrCodeLeaseConflict, operation).
				WithMission(m.MissionID, m.Generation, m.State).
				WithReasons("resource_busy")
		}

		// Advance state to leases_held.
		m.State = domain.StateLeasesHeld
		m.UpdatedAtTick = tick
		if uerr := tx.UpdateMission(ctx, m, m.AggregateVersion); uerr != nil {
			if uerr == store.ErrVersionConflict {
				return domain.NewError(domain.ErrCodeFinalAlreadyCommitted, operation).
					WithMission(m.MissionID, m.Generation, m.State)
			}
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 200); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		return a.buildLeaseResult(ctx, tx, m, &result)
	})
	if err != nil {
		return LeaseResult{}, err
	}
	return result, nil
}

func (a *Aggregate) buildLeaseResult(ctx context.Context, tx store.Tx, m domain.Mission, result *LeaseResult) error {
	leases, err := tx.GetLeases(ctx, m.MissionID)
	if err != nil {
		return domain.NewError(domain.ErrCodeInternal, "acquire_leases")
	}
	result.MissionID = m.MissionID
	result.Generation = m.Generation
	result.State = m.State
	result.Leases = leases
	return nil
}

// routeGenerationKey derives the immutable route-package generation resource
// key from the mission's route hash and generation.
func routeGenerationKey(m domain.Mission) string {
	return m.MissionID + "/" + m.RouteHash[:minInt(len(m.RouteHash), 16)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
