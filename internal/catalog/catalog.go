// Package catalog implements the AUV and Sea-State Rule Catalog: the immutable
// in-memory seed catalog plus the reference data used to validate lock
// requests, voyage-hull fit, sea-state freshness, integer capability limits,
// and reviewer qualifications.
package catalog

import "abyssalauvreleasegate/internal/domain"

// Voyage is a vessel voyage available for a mission generation.
type Voyage struct {
	VoyageID        string `json:"voyage_id"`
	VesselName      string `json:"vessel_name"`
	CurrentRevision int64  `json:"current_revision"`
}

// AUVHull describes the capabilities and constraints of an AUV hull.
type AUVHull struct {
	HullID         string `json:"hull_id"`
	VoyageID       string `json:"voyage_id"`
	MaxDepthM      int64  `json:"max_depth_m"`
	MaxSpeedCmS    int64  `json:"max_speed_cm_s"`
	EnergyBudgetWh int64  `json:"energy_budget_wh"`
	TrimMinG       int64  `json:"trim_min_g"`
	TrimMaxG       int64  `json:"trim_max_g"`
}

// BeaconSlot is an acoustic beacon slot available for reservation.
type BeaconSlot struct {
	SlotID      string `json:"slot_id"`
	FrequencyHz int64  `json:"frequency_hz"`
}

// SeaStateWindow is a sea-state revision bound to a voyage.
type SeaStateWindow struct {
	VoyageID        string `json:"voyage_id"`
	Revision        int64  `json:"revision"`
	WindowOpenTick  int64  `json:"window_open_tick"`
	WindowCloseTick int64  `json:"window_close_tick"`
}

// Reviewer describes a qualified signer for release operations.
type Reviewer struct {
	ReviewerID string              `json:"reviewer_id"`
	Roles      []domain.SignerRole `json:"roles"`
}

// AdapterEndpoint locates an external integration for a kind of call.
type AdapterEndpoint struct {
	Kind     domain.AdapterKind `json:"kind"`
	Endpoint string             `json:"endpoint"`
}

// Catalog is the read-only rule catalog used during lock and review validation.
type Catalog interface {
	// Voyage returns the named voyage, or false if unknown.
	Voyage(voyageID string) (Voyage, bool)
	// Hull returns the named hull, or false if unknown.
	Hull(hullID string) (AUVHull, bool)
	// HullFitsVoyage reports whether the hull belongs to the voyage.
	HullFitsVoyage(hullID, voyageID string) bool
	// SeaState returns the sea-state window for a voyage revision.
	SeaState(voyageID string, revision int64) (SeaStateWindow, bool)
	// SeaStateCurrent reports whether revision is the current revision.
	SeaStateCurrent(voyageID string, revision int64) bool
	// Reviewer returns a qualified signer, or false if unknown or unqualified.
	Reviewer(reviewerID string, role domain.SignerRole) (Reviewer, bool)
	// BeaconSlot returns the named beacon slot, or false if unknown.
	BeaconSlot(slotID string) (BeaconSlot, bool)
}
