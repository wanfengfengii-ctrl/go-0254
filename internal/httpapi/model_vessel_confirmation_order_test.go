package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"abyssalauvreleasegate/internal/domain"
)

func TestModel_VesselConfirmationCompletesReleaseGateInEitherSubmissionOrder(t *testing.T) {
	tests := []struct {
		name  string
		order []string
	}{
		{name: "vessel confirmation before reviews", order: []string{"vessel", "reviews"}},
		{name: "reviews before vessel confirmation", order: []string{"reviews", "vessel"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestServer(t)
			mustPost := func(path string, body any) []byte {
				t.Helper()
				status, response := postJSON(t, h, path, body)
				if status != http.StatusOK && status != http.StatusCreated {
					t.Fatalf("POST %s status = %d; body=%s", path, status, response)
				}
				return response
			}

			body := mustPost("/api/missions", map[string]any{
				"voyage_id": "VGR-01", "auv_hull_id": "HULL-01", "beacon_slot_id": "BEACON-01",
				"sea_state_revision": 1, "return_threshold_wh": 10000, "depth_limit_m": 4000,
				"trim_min_g": -400, "trim_max_g": 400,
				"waypoints": []map[string]any{
					{"seq": 1, "latitude_microdeg": -12300000, "longitude_microdeg": 4500000, "target_depth_m": 3000, "max_speed_cm_s": 120, "expected_draw_wh": 4000, "dwell_seconds": 60},
					{"seq": 2, "latitude_microdeg": -12350000, "longitude_microdeg": 4520000, "target_depth_m": 3500, "max_speed_cm_s": 120, "expected_draw_wh": 5000, "dwell_seconds": 60},
				},
			})
			var mission domain.Mission
			if err := json.Unmarshal(body, &mission); err != nil {
				t.Fatalf("decode mission: %v", err)
			}
			base := "/api/missions/" + mission.MissionID

			for i, authorization := range []struct {
				signer string
				role   domain.SignerRole
			}{
				{signer: "eng-alice", role: domain.RoleTechnicalAuthorizer},
				{signer: "eng-bob", role: domain.RoleSafetyAuthorizer},
			} {
				mustPost(base+"/authorizations", map[string]any{
					"generation": 1, "op_key": fmt.Sprintf("auth-%d", i),
					"signer_id": authorization.signer, "role": authorization.role,
					"payload_hash": mission.LockedPayloadHash,
				})
			}
			mustPost(base+"/leases/acquire", map[string]any{"generation": 1, "op_key": "leases"})
			mustPost(base+"/segments/1/simulate", map[string]any{
				"generation": 1, "op_key": "segment", "simulator_script_ref": "sim-script-01",
				"adapter_status": "success", "verdict": "pass",
			})
			mustPost(base+"/margins/check", map[string]any{"generation": 1, "op_key": "margins"})

			recordVessel := func() {
				mustPost(base+"/vessel-confirmations", map[string]any{
					"generation": 1, "op_key": "vessel", "adapter_status": "success",
					"response_hash": "vessel-success-response",
				})
			}
			recordReviews := func() {
				reviewers := []string{"eng-alice", "eng-alice", "sea-carol", "sea-carol"}
				kinds := []string{"route", "margins", "vessel_confirmation", "sea_state"}
				for i, kind := range kinds {
					mustPost(base+"/reviews", map[string]any{
						"generation": 1, "op_key": fmt.Sprintf("review-%d", i),
						"reviewer_id": reviewers[i], "review_kind": kind,
						"decision": "pass", "evidence_hash": fmt.Sprintf("evidence-%d", i),
					})
				}
			}

			for _, step := range tt.order {
				if step == "vessel" {
					recordVessel()
				} else {
					recordReviews()
				}
			}

			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("GET mission status = %d; body=%s", response.Code, response.Body.String())
			}
			var detail domain.MissionDetail
			if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
				t.Fatalf("decode mission detail: %v", err)
			}
			if !detail.VesselConfirmed || !detail.ReviewComplete {
				t.Fatalf("persisted gates: vessel_confirmed=%v review_complete=%v; want both true", detail.VesselConfirmed, detail.ReviewComplete)
			}
			if detail.State != domain.StateLaunchable {
				t.Fatalf("persisted state with both gates complete = %q; want %q", detail.State, domain.StateLaunchable)
			}
		})
	}
}
