package catalog

import (
	"strconv"

	"abyssalauvreleasegate/internal/domain"
)

// Seed is an immutable in-memory catalog implementation used for tests and as
// the default backing store for the HTTP service.
type Seed struct {
	voyages   map[string]Voyage
	hulls     map[string]AUVHull
	seaStates map[string]SeaStateWindow
	reviewers map[string]Reviewer
	slots     map[string]BeaconSlot
}

// NewSeed builds an immutable catalog from the supplied reference data.
func NewSeed(voyages []Voyage, hulls []AUVHull, seaStates []SeaStateWindow, reviewers []Reviewer, slots []BeaconSlot) *Seed {
	s := &Seed{
		voyages:   make(map[string]Voyage, len(voyages)),
		hulls:     make(map[string]AUVHull, len(hulls)),
		seaStates: make(map[string]SeaStateWindow, len(seaStates)),
		reviewers: make(map[string]Reviewer, len(reviewers)),
		slots:     make(map[string]BeaconSlot, len(slots)),
	}
	for _, v := range voyages {
		s.voyages[v.VoyageID] = v
	}
	for _, h := range hulls {
		s.hulls[h.HullID] = h
	}
	for _, ss := range seaStates {
		s.seaStates[seaStateKey(ss.VoyageID, ss.Revision)] = ss
	}
	for _, r := range reviewers {
		s.reviewers[r.ReviewerID] = r
	}
	for _, b := range slots {
		s.slots[b.SlotID] = b
	}
	return s
}

func seaStateKey(voyageID string, revision int64) string {
	return voyageID + "/" + strconv.FormatInt(revision, 10)
}

func (s *Seed) Voyage(voyageID string) (Voyage, bool) {
	v, ok := s.voyages[voyageID]
	return v, ok
}

func (s *Seed) Hull(hullID string) (AUVHull, bool) {
	h, ok := s.hulls[hullID]
	return h, ok
}

func (s *Seed) HullFitsVoyage(hullID, voyageID string) bool {
	h, ok := s.hulls[hullID]
	if !ok {
		return false
	}
	return h.VoyageID == voyageID
}

func (s *Seed) SeaState(voyageID string, revision int64) (SeaStateWindow, bool) {
	ss, ok := s.seaStates[seaStateKey(voyageID, revision)]
	return ss, ok
}

func (s *Seed) SeaStateCurrent(voyageID string, revision int64) bool {
	v, ok := s.voyages[voyageID]
	if !ok {
		return false
	}
	return v.CurrentRevision == revision
}

func (s *Seed) Reviewer(reviewerID string, role domain.SignerRole) (Reviewer, bool) {
	r, ok := s.reviewers[reviewerID]
	if !ok {
		return Reviewer{}, false
	}
	for _, rr := range r.Roles {
		if rr == role {
			return r, true
		}
	}
	return Reviewer{}, false
}

func (s *Seed) BeaconSlot(slotID string) (BeaconSlot, bool) {
	b, ok := s.slots[slotID]
	return b, ok
}

// DefaultSeed returns a small deterministic reference catalog for the trial.
func DefaultSeed() *Seed {
	return NewSeed(
		[]Voyage{{VoyageID: "VGR-01", VesselName: "RV Abyssal", CurrentRevision: 1}},
		[]AUVHull{
			{HullID: "HULL-01", VoyageID: "VGR-01", MaxDepthM: 6000, MaxSpeedCmS: 200, EnergyBudgetWh: 50000, TrimMinG: -500, TrimMaxG: 500},
			{HullID: "HULL-02", VoyageID: "VGR-01", MaxDepthM: 3000, MaxSpeedCmS: 150, EnergyBudgetWh: 30000, TrimMinG: -300, TrimMaxG: 300},
		},
		[]SeaStateWindow{
			{VoyageID: "VGR-01", Revision: 1, WindowOpenTick: 0, WindowCloseTick: 1000000},
			{VoyageID: "VGR-01", Revision: 2, WindowOpenTick: 500000, WindowCloseTick: 1500000},
		},
		[]Reviewer{
			{ReviewerID: "eng-alice", Roles: []domain.SignerRole{domain.RoleTechnicalAuthorizer, domain.RoleIndependentReviewer}},
			{ReviewerID: "eng-bob", Roles: []domain.SignerRole{domain.RoleSafetyAuthorizer, domain.RoleIndependentReviewer}},
			{ReviewerID: "sea-carol", Roles: []domain.SignerRole{domain.RoleIndependentReviewer}},
			{ReviewerID: "sea-dave", Roles: []domain.SignerRole{domain.RoleIndependentReviewer}},
		},
		[]BeaconSlot{
			{SlotID: "BEACON-01", FrequencyHz: 27000},
			{SlotID: "BEACON-02", FrequencyHz: 28000},
		},
	)
}
