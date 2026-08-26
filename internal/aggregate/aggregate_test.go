package aggregate_test

import (
	"context"
	"errors"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

func newService(t *testing.T) (*aggregate.Aggregate, *store.SQLite) {
	t.Helper()
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	var tick int64
	var id int
	svc := aggregate.New(catalog.DefaultSeed(), st,
		func() int64 { tick++; return tick },
		func() string { id++; return "M-" + itoa(int64(id)) },
	)
	return svc, st
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func validRequest() aggregate.LockRequest {
	return aggregate.LockRequest{
		VoyageID:          "VGR-01",
		AUVHullID:         "HULL-01",
		BeaconSlotID:      "BEACON-01",
		SeaStateRevision:  1,
		ReturnThresholdWh: 10000,
		DepthLimitM:       4000,
		TrimMinG:          -400,
		TrimMaxG:          400,
		Waypoints: []aggregate.WaypointInput{
			{Seq: 1, LatitudeMicrodeg: -12300000, LongitudeMicrodeg: 4500000, TargetDepthM: 3000, MaxSpeedCmS: 120, ExpectedDrawWh: 4000, DwellSeconds: 60},
			{Seq: 2, LatitudeMicrodeg: -12350000, LongitudeMicrodeg: 4520000, TargetDepthM: 3500, MaxSpeedCmS: 120, ExpectedDrawWh: 5000, DwellSeconds: 60},
		},
	}
}

func TestLockMissionSucceeds(t *testing.T) {
	svc, _ := newService(t)
	m, err := svc.LockMission(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if m.MissionID == "" {
		t.Error("mission id should be assigned")
	}
	if m.Generation != 1 {
		t.Errorf("generation = %d, want 1", m.Generation)
	}
	if m.State != domain.StatePendingAuthorization {
		t.Errorf("state = %q, want pending_authorization", m.State)
	}
	if m.RouteHash == "" || m.LockedPayloadHash == "" {
		t.Error("hashes should be non-empty")
	}
	if len(m.Waypoints) != 2 {
		t.Errorf("waypoints = %d, want 2", len(m.Waypoints))
	}
}

func TestLockRejectsHullVoyageMismatch(t *testing.T) {
	// Build a catalog where HULL-01 belongs to a different voyage.
	cat := catalog.NewSeed(
		[]catalog.Voyage{{VoyageID: "VGR-01", VesselName: "RV Abyssal", CurrentRevision: 1}},
		[]catalog.AUVHull{{HullID: "HULL-01", VoyageID: "VGR-99", MaxDepthM: 6000, MaxSpeedCmS: 200, EnergyBudgetWh: 50000, TrimMinG: -500, TrimMaxG: 500}},
		[]catalog.SeaStateWindow{{VoyageID: "VGR-01", Revision: 1, WindowOpenTick: 0, WindowCloseTick: 1000000}},
		nil,
		[]catalog.BeaconSlot{{SlotID: "BEACON-01", FrequencyHz: 27000}},
	)
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	svc := aggregate.New(cat, st, func() int64 { return 1 }, func() string { return "M-1" })

	_, err = svc.LockMission(context.Background(), validRequest())
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeValidation {
		t.Errorf("code = %q, want validation", de.Code)
	}
}

func TestLockRejectsStaleSeaState(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.SeaStateRevision = 99
	_, err := svc.LockMission(context.Background(), req)
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeValidation {
		t.Errorf("code = %q, want validation", de.Code)
	}
}

func TestLockRejectsDepthBeyondEnvelope(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.DepthLimitM = 7000 // HULL-01 max depth is 6000
	_, err := svc.LockMission(context.Background(), req)
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeValidation {
		t.Errorf("code = %q, want validation", de.Code)
	}
}

func TestLockRejectsWaypointSequenceGap(t *testing.T) {
	svc, _ := newService(t)
	req := validRequest()
	req.Waypoints[1].Seq = 3
	_, err := svc.LockMission(context.Background(), req)
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeValidation {
		t.Errorf("code = %q, want validation", de.Code)
	}
}

func TestGetMissionRoundTrip(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.LockMission(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	got, err := svc.GetMission(context.Background(), created.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MissionID != created.MissionID {
		t.Errorf("id mismatch: %q vs %q", got.MissionID, created.MissionID)
	}
	if got.RouteHash != created.RouteHash {
		t.Errorf("route hash mismatch")
	}
	if len(got.Waypoints) != 2 {
		t.Errorf("waypoints = %d, want 2", len(got.Waypoints))
	}
}

func TestGetMissingMission(t *testing.T) {
	svc, _ := newService(t)
	_, err := svc.GetMission(context.Background(), "MISSING")
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if de.Code != domain.ErrCodeNotFound {
		t.Errorf("code = %q, want not_found", de.Code)
	}
}
