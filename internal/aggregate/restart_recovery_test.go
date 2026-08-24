package aggregate_test

import (
	"context"
	"path/filepath"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// newFileService opens a file-backed service with a monotonic logical clock.
func newFileService(t *testing.T, path string, tick int64) (*aggregate.Aggregate, *store.SQLite) {
	t.Helper()
	st, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	clock := tick
	var id int
	svc := aggregate.New(catalog.DefaultSeed(), st,
		func() int64 { clock++; return clock },
		func() string { id++; return "M-" + itoa(int64(id)) },
	)
	return svc, st
}

// TestRestartRecovery persists leases, a partial prefix, retry attempts, and
// reviews, then restarts over the same SQLite file and asserts deterministic
// reconstruction and continued behavior.
func TestRestartRecovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auvgate.db")

	svc1, st1 := newFileService(t, path, 0)
	m := lockedAndLeased(t, svc1, threeSegmentRequest())

	// Accept segment 1, fail segment 2 once, then leave segment 2 pending.
	if _, err := svc1.SimulateSegment(ctx, m.MissionID, 1, aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}); err != nil {
		t.Fatalf("seg1: %v", err)
	}
	if _, err := svc1.SimulateSegment(ctx, m.MissionID, 2, aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictFail, ReasonCode: "sim_fault",
	}); err != nil {
		t.Fatalf("seg2 fail: %v", err)
	}
	st1.Close()

	// Restart over the same file.
	svc2, st2 := newFileService(t, path, 100)
	defer st2.Close()

	detail, err := svc2.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if detail.State != domain.StateRouteChecking {
		t.Fatalf("state after restart = %q, want route_checking", detail.State)
	}
	if len(detail.Leases) != 3 {
		t.Fatalf("leases after restart = %d, want 3", len(detail.Leases))
	}
	if detail.RoutePrefix != 1 {
		t.Fatalf("route prefix after restart = %d, want 1", detail.RoutePrefix)
	}
	if len(detail.SegmentEvidence) != 2 {
		t.Fatalf("segment evidence after restart = %d, want 2", len(detail.SegmentEvidence))
	}
	if len(detail.Authorizations) != 2 {
		t.Fatalf("authorizations after restart = %d, want 2", len(detail.Authorizations))
	}

	// Continued behavior: retry segment 2 successfully.
	if _, err := svc2.SimulateSegment(ctx, m.MissionID, 2, aggregate.SimulateRequest{
		Generation: m.Generation, SimulatorScriptRef: "s", AdapterStatus: domain.AttemptSuccess, Verdict: domain.VerdictPass,
	}); err != nil {
		t.Fatalf("seg2 retry: %v", err)
	}
	detail, err = svc2.GetMission(ctx, m.MissionID)
	if err != nil {
		t.Fatalf("get after retry: %v", err)
	}
	if detail.RoutePrefix != 2 {
		t.Fatalf("route prefix after retry = %d, want 2", detail.RoutePrefix)
	}
	if detail.State != domain.StateMarginChecking {
		t.Fatalf("state after prefix complete = %q, want margin_checking", detail.State)
	}
}
