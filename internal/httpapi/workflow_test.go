package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"abyssalauvreleasegate/internal/domain"
)

func postJSON(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	return rec.Code, rec.Body.Bytes()
}

// TestHTTPWorkflow drives the full release gate over the HTTP API, exercising
// every route and the deterministic error envelope.
func TestHTTPWorkflow(t *testing.T) {
	h := newTestServer(t)

	status, body := postJSON(t, h, "/api/missions", map[string]any{
		"voyage_id": "VGR-01", "auv_hull_id": "HULL-01", "beacon_slot_id": "BEACON-01",
		"sea_state_revision": 1, "return_threshold_wh": 10000, "depth_limit_m": 4000,
		"trim_min_g": -400, "trim_max_g": 400,
		"waypoints": []map[string]any{
			{"seq": 1, "latitude_microdeg": -12300000, "longitude_microdeg": 4500000, "target_depth_m": 3000, "max_speed_cm_s": 120, "expected_draw_wh": 4000, "dwell_seconds": 60},
			{"seq": 2, "latitude_microdeg": -12350000, "longitude_microdeg": 4520000, "target_depth_m": 3500, "max_speed_cm_s": 120, "expected_draw_wh": 5000, "dwell_seconds": 60},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("lock status = %d; body=%s", status, body)
	}
	var m domain.Mission
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode mission: %v", err)
	}
	base := "/api/missions/" + m.MissionID

	// Two-person technical authorization.
	for i, a := range []struct {
		signer string
		role   domain.SignerRole
	}{{"eng-alice", domain.RoleTechnicalAuthorizer}, {"eng-bob", domain.RoleSafetyAuthorizer}} {
		status, body = postJSON(t, h, base+"/authorizations", map[string]any{
			"generation": 1, "op_key": fmt.Sprintf("auth-%d", i), "signer_id": a.signer,
			"role": string(a.role), "payload_hash": m.LockedPayloadHash,
		})
		if status != http.StatusOK {
			t.Fatalf("authorize %s status = %d; body=%s", a.signer, status, body)
		}
	}

	if status, _ = postJSON(t, h, base+"/leases/acquire", map[string]any{"generation": 1, "op_key": "lease-1"}); status != http.StatusOK {
		t.Fatalf("lease status = %d", status)
	}

	if status, _ = postJSON(t, h, base+"/segments/1/simulate", map[string]any{
		"generation": 1, "op_key": "seg-1", "simulator_script_ref": "s",
		"adapter_status": "success", "verdict": "pass",
	}); status != http.StatusOK {
		t.Fatalf("simulate status = %d", status)
	}

	if status, _ = postJSON(t, h, base+"/margins/check", map[string]any{"generation": 1, "op_key": "margin-1"}); status != http.StatusOK {
		t.Fatalf("margins status = %d", status)
	}

	if status, _ = postJSON(t, h, base+"/vessel-confirmations", map[string]any{
		"generation": 1, "op_key": "v-1", "adapter_status": "success", "response_hash": "reply",
	}); status != http.StatusOK {
		t.Fatalf("vessel status = %d", status)
	}

	reviewers := []string{"eng-alice", "eng-alice", "sea-carol", "sea-carol"}
	kinds := []string{"route", "margins", "vessel_confirmation", "sea_state"}
	for i, kind := range kinds {
		if status, _ = postJSON(t, h, base+"/reviews", map[string]any{
			"generation": 1, "op_key": fmt.Sprintf("rev-%d", i), "reviewer_id": reviewers[i],
			"review_kind": kind, "decision": "pass", "evidence_hash": "e",
		}); status != http.StatusOK {
			t.Fatalf("review %s status = %d", kind, status)
		}
	}

	status, body = postJSON(t, h, base+"/finalize", map[string]any{"generation": 1, "op_key": "fin-1", "result": "launch"})
	if status != http.StatusOK {
		t.Fatalf("finalize status = %d; body=%s", status, body)
	}
	var outcome domain.FinalOutcome
	if err := json.Unmarshal(body, &outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome.CredentialNonce == "" {
		t.Fatal("launch should carry a credential")
	}
}
