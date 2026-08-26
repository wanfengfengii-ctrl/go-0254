package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

func testMission() domain.Mission {
	return domain.Mission{
		MissionID:         "M-1",
		VoyageID:          "VGR-01",
		AUVHullID:         "HULL-01",
		Generation:        1,
		State:             domain.StatePendingAuthorization,
		LockedPayloadHash: "locked",
		RouteHash:         "route",
		SeaStateRevision:  1,
		ReturnThresholdWh: 10000,
		DepthLimitM:       4000,
		TrimMinG:          -400,
		TrimMaxG:          400,
		AggregateVersion:  1,
		CreatedAtTick:     1,
		UpdatedAtTick:     1,
	}
}

func testWaypoints() []domain.Waypoint {
	return []domain.Waypoint{
		{MissionID: "M-1", Generation: 1, Seq: 1, LatitudeMicrodeg: -12300000, LongitudeMicrodeg: 4500000, TargetDepthM: 3000, MaxSpeedCmS: 120, ExpectedDrawWh: 4000, DwellSeconds: 60, Checksum: "c1"},
		{MissionID: "M-1", Generation: 1, Seq: 2, LatitudeMicrodeg: -12350000, LongitudeMicrodeg: 4520000, TargetDepthM: 3500, MaxSpeedCmS: 120, ExpectedDrawWh: 5000, DwellSeconds: 60, Checksum: "c2"},
	}
}

func TestCreateAndGetMission(t *testing.T) {
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.CreateMission(ctx, testMission(), testWaypoints()); err != nil {
		t.Fatalf("create: %v", err)
	}

	m, wps, err := st.GetMission(ctx, "M-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if m.MissionID != "M-1" {
		t.Errorf("mission id = %q, want M-1", m.MissionID)
	}
	if m.RouteHash != "route" {
		t.Errorf("route hash = %q, want route", m.RouteHash)
	}
	if len(wps) != 2 {
		t.Fatalf("waypoints = %d, want 2", len(wps))
	}
	if wps[0].Seq != 1 || wps[1].Seq != 2 {
		t.Errorf("waypoints not ordered: %d, %d", wps[0].Seq, wps[1].Seq)
	}
}

func TestGetMissingMission(t *testing.T) {
	st, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if _, _, err := st.GetMission(context.Background(), "NOPE"); err != store.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auvgate.db")

	st1, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := st1.CreateMission(context.Background(), testMission(), testWaypoints()); err != nil {
		t.Fatalf("create: %v", err)
	}
	st1.Close()

	// Reopen over the same file to simulate restart recovery.
	st2, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer st2.Close()

	m, wps, err := st2.GetMission(context.Background(), "M-1")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if m.State != domain.StatePendingAuthorization {
		t.Errorf("state = %q, want pending_authorization", m.State)
	}
	if len(wps) != 2 {
		t.Errorf("waypoints after restart = %d, want 2", len(wps))
	}
}
