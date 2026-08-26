// Package aggregate implements the Deployment Mission Aggregate: the state
// transitions, irreversible generation barrier, idempotency keys, sorted
// rejection reasons, terminal-state fence, lease capture, ordered segment
// simulation, integer margin checks, vessel confirmation, independent review,
// final arbitration, and restart reconstruction documented in the plan.
package aggregate

import (
	"context"
	"errors"

	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// MaxWaypointCount bounds the waypoint list length accepted at lock time.
const MaxWaypointCount int64 = 1000

// WaypointInput is one ordered waypoint supplied by the operator at lock time.
type WaypointInput struct {
	Seq               int64 `json:"seq"`
	LatitudeMicrodeg  int64 `json:"latitude_microdeg"`
	LongitudeMicrodeg int64 `json:"longitude_microdeg"`
	TargetDepthM      int64 `json:"target_depth_m"`
	MaxSpeedCmS       int64 `json:"max_speed_cm_s"`
	ExpectedDrawWh    int64 `json:"expected_draw_wh"`
	DwellSeconds      int64 `json:"dwell_seconds"`
}

// LockRequest captures the frozen parameters for a new mission generation.
type LockRequest struct {
	VoyageID          string          `json:"voyage_id"`
	AUVHullID         string          `json:"auv_hull_id"`
	BeaconSlotID      string          `json:"beacon_slot_id"`
	SeaStateRevision  int64           `json:"sea_state_revision"`
	ReturnThresholdWh int64           `json:"return_threshold_wh"`
	DepthLimitM       int64           `json:"depth_limit_m"`
	TrimMinG          int64           `json:"trim_min_g"`
	TrimMaxG          int64           `json:"trim_max_g"`
	Waypoints         []WaypointInput `json:"waypoints"`
}

// Service is the deployment mission aggregate boundary exposed to the HTTP API.
type Service interface {
	// LockMission freezes a new mission generation after catalog and
	// integer-bound validation, returning the persisted aggregate.
	LockMission(ctx context.Context, req LockRequest) (domain.Mission, error)

	// GetMission returns the reconstructed aggregate, leases, evidence chains,
	// retry queue, review status, and final result for the operations console.
	GetMission(ctx context.Context, missionID string) (domain.MissionDetail, error)

	// Authorize records one technical authorization for the locked generation.
	Authorize(ctx context.Context, missionID string, req AuthorizeRequest) (AuthorizationResult, error)

	// AcquireLeases atomically captures hull, beacon-slot, and route-generation
	// leases for the authorized mission.
	AcquireLeases(ctx context.Context, missionID string, req LeaseRequest) (LeaseResult, error)

	// SimulateSegment submits or retries simulator evidence for one segment.
	SimulateSegment(ctx context.Context, missionID string, seq int64, req SimulateRequest) (SegmentResult, error)

	// CheckMargins runs the deterministic return-safety arithmetic.
	CheckMargins(ctx context.Context, missionID string, req MarginRequest) (MarginResult, error)

	// RecordVesselConfirmation records a vessel adapter attempt.
	RecordVesselConfirmation(ctx context.Context, missionID string, req VesselRequest) (VesselResult, error)

	// RecordReview records independent review evidence.
	RecordReview(ctx context.Context, missionID string, req ReviewRequest) (ReviewResult, error)

	// Finalize commits launch, isolation, or cancellation through the final
	// arbiter, returning the unique credential only for a successful launch.
	Finalize(ctx context.Context, missionID string, req FinalizeRequest) (domain.FinalOutcome, error)
}

// Aggregate implements Service over a rule catalog and a persistence store.
type Aggregate struct {
	cat   catalog.Catalog
	store store.Store
	now   func() int64
	newID func() string
}

// New builds an Aggregate with the supplied catalog, store, logical clock, and
// identifier generator.
func New(cat catalog.Catalog, st store.Store, now func() int64, newID func() string) *Aggregate {
	return &Aggregate{cat: cat, store: st, now: now, newID: newID}
}

// LockMission validates the request and persists a new pending_authorization
// mission generation with immutable hashes.
func (a *Aggregate) LockMission(ctx context.Context, req LockRequest) (domain.Mission, error) {
	var reasons []string

	if _, ok := a.cat.Voyage(req.VoyageID); !ok {
		reasons = append(reasons, "unknown_voyage")
	}
	hull, hullOK := a.cat.Hull(req.AUVHullID)
	if !hullOK {
		reasons = append(reasons, "unknown_hull")
	} else if !a.cat.HullFitsVoyage(req.AUVHullID, req.VoyageID) {
		reasons = append(reasons, "hull_voyage_mismatch")
	}
	if !a.cat.SeaStateCurrent(req.VoyageID, req.SeaStateRevision) {
		reasons = append(reasons, "stale_sea_state_revision")
	}
	if _, ok := a.cat.BeaconSlot(req.BeaconSlotID); !ok {
		reasons = append(reasons, "unknown_beacon_slot")
	}

	// Integer-bound validation.
	if req.DepthLimitM <= 0 {
		reasons = append(reasons, "depth_limit_nonpositive")
	} else if hullOK && req.DepthLimitM > hull.MaxDepthM {
		reasons = append(reasons, "depth_beyond_envelope")
	}
	if req.ReturnThresholdWh < 0 {
		reasons = append(reasons, "negative_return_threshold")
	}
	if req.TrimMinG > req.TrimMaxG {
		reasons = append(reasons, "trim_bounds_inverted")
	} else if hullOK && (req.TrimMinG < hull.TrimMinG || req.TrimMaxG > hull.TrimMaxG) {
		reasons = append(reasons, "trim_outside_bounds")
	}
	if int64(len(req.Waypoints)) <= 0 {
		reasons = append(reasons, "empty_waypoint_list")
	} else if int64(len(req.Waypoints)) > MaxWaypointCount {
		reasons = append(reasons, "excessive_waypoint_count")
	}

	// Waypoint integer bounds and sequence ordering.
	for i, w := range req.Waypoints {
		if w.Seq != int64(i+1) {
			reasons = append(reasons, "waypoint_sequence_gap")
			break
		}
		if w.TargetDepthM < 0 {
			reasons = append(reasons, "negative_waypoint_depth")
		}
		if hullOK && w.TargetDepthM > hull.MaxDepthM {
			reasons = append(reasons, "waypoint_depth_beyond_envelope")
		}
		if w.MaxSpeedCmS < 0 {
			reasons = append(reasons, "negative_waypoint_speed")
		}
		if hullOK && w.MaxSpeedCmS > hull.MaxSpeedCmS {
			reasons = append(reasons, "waypoint_speed_beyond_capability")
		}
		if w.DwellSeconds < 0 {
			reasons = append(reasons, "negative_dwell")
		}
		if w.ExpectedDrawWh < 0 {
			reasons = append(reasons, "negative_energy_draw")
		}
	}

	if len(reasons) > 0 {
		return domain.Mission{}, domain.NewError(domain.ErrCodeValidation, "lock_mission").
			WithReasons(domain.SortedReasons(reasons...)...)
	}

	// Normalize waypoints into persisted domain values.
	missionID := a.newID()
	tick := a.now()
	waypoints := make([]domain.Waypoint, 0, len(req.Waypoints))
	routeParts := make([]routePart, 0, len(req.Waypoints))
	for _, w := range req.Waypoints {
		wp := domain.Waypoint{
			MissionID:         missionID,
			Generation:        1,
			Seq:               w.Seq,
			LatitudeMicrodeg:  w.LatitudeMicrodeg,
			LongitudeMicrodeg: w.LongitudeMicrodeg,
			TargetDepthM:      w.TargetDepthM,
			MaxSpeedCmS:       w.MaxSpeedCmS,
			ExpectedDrawWh:    w.ExpectedDrawWh,
			DwellSeconds:      w.DwellSeconds,
		}
		wp.Checksum, _ = domain.HashJSON(wp)
		waypoints = append(waypoints, wp)
		routeParts = append(routeParts, routePart{
			Seq:               wp.Seq,
			LatitudeMicrodeg:  wp.LatitudeMicrodeg,
			LongitudeMicrodeg: wp.LongitudeMicrodeg,
			TargetDepthM:      wp.TargetDepthM,
			MaxSpeedCmS:       wp.MaxSpeedCmS,
			ExpectedDrawWh:    wp.ExpectedDrawWh,
			DwellSeconds:      wp.DwellSeconds,
		})
	}

	routeHash, err := domain.HashJSON(routeParts)
	if err != nil {
		return domain.Mission{}, domain.NewError(domain.ErrCodeInternal, "lock_mission")
	}

	payload := lockedPayload{
		VoyageID:          req.VoyageID,
		AUVHullID:         req.AUVHullID,
		BeaconSlotID:      req.BeaconSlotID,
		SeaStateRevision:  req.SeaStateRevision,
		ReturnThresholdWh: req.ReturnThresholdWh,
		DepthLimitM:       req.DepthLimitM,
		TrimMinG:          req.TrimMinG,
		TrimMaxG:          req.TrimMaxG,
		RouteHash:         routeHash,
	}
	lockedHash, err := domain.HashJSON(payload)
	if err != nil {
		return domain.Mission{}, domain.NewError(domain.ErrCodeInternal, "lock_mission")
	}

	m := domain.Mission{
		MissionID:         missionID,
		VoyageID:          req.VoyageID,
		AUVHullID:         req.AUVHullID,
		BeaconSlotID:      req.BeaconSlotID,
		Generation:        1,
		State:             domain.StatePendingAuthorization,
		LockedPayloadHash: lockedHash,
		RouteHash:         routeHash,
		SeaStateRevision:  req.SeaStateRevision,
		ReturnThresholdWh: req.ReturnThresholdWh,
		DepthLimitM:       req.DepthLimitM,
		TrimMinG:          req.TrimMinG,
		TrimMaxG:          req.TrimMaxG,
		AggregateVersion:  1,
		CreatedAtTick:     tick,
		UpdatedAtTick:     tick,
		Waypoints:         waypoints,
	}

	if err := a.store.CreateMission(ctx, m, waypoints); err != nil {
		return domain.Mission{}, domain.NewError(domain.ErrCodeInternal, "lock_mission")
	}
	return m, nil
}

// GetMission returns the reconstructed aggregate and its ordered waypoints.
func (a *Aggregate) GetMission(ctx context.Context, missionID string) (domain.MissionDetail, error) {
	m, waypoints, err := a.store.GetMission(ctx, missionID)
	if errors.Is(err, store.ErrNotFound) {
		return domain.MissionDetail{}, domain.NewError(domain.ErrCodeNotFound, "get_mission").
			WithMission(missionID, 0, "")
	}
	if err != nil {
		return domain.MissionDetail{}, domain.NewError(domain.ErrCodeInternal, "get_mission")
	}
	m.Waypoints = waypoints

	detail := domain.MissionDetail{Mission: m}
	detail.Leases, _ = a.store.GetLeases(ctx, missionID)
	detail.Authorizations, _ = a.store.GetAuthorizations(ctx, missionID, m.Generation)
	detail.SegmentEvidence, _ = a.store.GetSegmentEvidence(ctx, missionID, m.Generation)
	detail.MarginEvidence, _ = a.store.GetMarginEvidence(ctx, missionID, m.Generation)
	detail.AdapterAttempts, _ = a.store.GetAdapterAttempts(ctx, missionID, m.Generation)
	detail.Reviews, _ = a.store.GetReviews(ctx, missionID, m.Generation)
	detail.RoutePrefix = acceptedPrefix(detail.SegmentEvidence, int64(len(waypoints))-1)
	detail.VesselConfirmed = vesselConfirmed(detail.AdapterAttempts)
	detail.ReviewComplete = reviewComplete(detail.Reviews)
	if final, ok, _ := a.store.GetFinal(ctx, missionID, m.Generation); ok {
		detail.FinalCredential = final.CredentialNonce
	}

	return detail, nil
}

// routePart is the canonical serialization unit for the immutable route hash.
type routePart struct {
	Seq               int64 `json:"seq"`
	LatitudeMicrodeg  int64 `json:"latitude_microdeg"`
	LongitudeMicrodeg int64 `json:"longitude_microdeg"`
	TargetDepthM      int64 `json:"target_depth_m"`
	MaxSpeedCmS       int64 `json:"max_speed_cm_s"`
	ExpectedDrawWh    int64 `json:"expected_draw_wh"`
	DwellSeconds      int64 `json:"dwell_seconds"`
}

// lockedPayload is the canonical serialization unit for the locked payload hash.
type lockedPayload struct {
	VoyageID          string `json:"voyage_id"`
	AUVHullID         string `json:"auv_hull_id"`
	BeaconSlotID      string `json:"beacon_slot_id"`
	SeaStateRevision  int64  `json:"sea_state_revision"`
	ReturnThresholdWh int64  `json:"return_threshold_wh"`
	DepthLimitM       int64  `json:"depth_limit_m"`
	TrimMinG          int64  `json:"trim_min_g"`
	TrimMaxG          int64  `json:"trim_max_g"`
	RouteHash         string `json:"route_hash"`
}
