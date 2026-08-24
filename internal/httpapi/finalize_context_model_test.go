package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"abyssalauvreleasegate/internal/domain"
)

func TestModel_FinalizeRequestContextControlsMutation(t *testing.T) {
	tests := []struct {
		name                 string
		cancelFinalize       bool
		wantState            domain.State
		wantTerminalResult   string
		wantFinalStatus      domain.LeaseStatus
		wantFinalizeHTTPCode int
	}{
		{
			name:                 "client canceled final cancellation cannot mutate state or release leases",
			cancelFinalize:       true,
			wantState:            domain.StateLeasesHeld,
			wantFinalStatus:      domain.LeaseOpen,
			wantFinalizeHTTPCode: http.StatusOK,
		},
		{
			name:                 "live final cancellation commits and releases leases",
			wantState:            domain.StateCancelled,
			wantTerminalResult:   string(domain.FinalCancelled),
			wantFinalStatus:      domain.LeaseReleased,
			wantFinalizeHTTPCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestServer(t)

			status, body := postJSON(t, h, "/api/missions", map[string]any{
				"voyage_id":           "VGR-01",
				"auv_hull_id":         "HULL-01",
				"beacon_slot_id":      "BEACON-01",
				"sea_state_revision":  1,
				"return_threshold_wh": 10000,
				"depth_limit_m":       4000,
				"trim_min_g":          -400,
				"trim_max_g":          400,
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

			for _, auth := range []map[string]any{
				{"generation": 1, "op_key": "auth-technical", "signer_id": "eng-alice", "role": string(domain.RoleTechnicalAuthorizer), "payload_hash": m.LockedPayloadHash},
				{"generation": 1, "op_key": "auth-safety", "signer_id": "eng-bob", "role": string(domain.RoleSafetyAuthorizer), "payload_hash": m.LockedPayloadHash},
			} {
				status, body = postJSON(t, h, base+"/authorizations", auth)
				if status != http.StatusOK {
					t.Fatalf("authorize status = %d; body=%s", status, body)
				}
			}

			status, body = postJSON(t, h, base+"/leases/acquire", map[string]any{"generation": 1, "op_key": "lease"})
			if status != http.StatusOK {
				t.Fatalf("lease status = %d; body=%s", status, body)
			}

			finalBody, err := json.Marshal(map[string]any{"generation": 1, "op_key": "final-cancel", "result": string(domain.FinalCancelled)})
			if err != nil {
				t.Fatalf("marshal finalize: %v", err)
			}
			finalReq := httptest.NewRequest(http.MethodPost, base+"/finalize", bytes.NewReader(finalBody))
			if tt.cancelFinalize {
				ctx, cancel := context.WithCancel(finalReq.Context())
				cancel()
				finalReq = finalReq.WithContext(ctx)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, finalReq)
			if !tt.cancelFinalize && rec.Code != tt.wantFinalizeHTTPCode {
				t.Fatalf("finalize status = %d; body=%s", rec.Code, rec.Body.String())
			}

			get := httptest.NewRecorder()
			h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, base, nil))
			if get.Code != http.StatusOK {
				t.Fatalf("get status = %d; body=%s", get.Code, get.Body.String())
			}
			var detail domain.MissionDetail
			if err := json.Unmarshal(get.Body.Bytes(), &detail); err != nil {
				t.Fatalf("decode detail: %v", err)
			}
			if detail.State != tt.wantState {
				t.Fatalf("state = %q, want %q", detail.State, tt.wantState)
			}
			if detail.TerminalResult != tt.wantTerminalResult {
				t.Fatalf("terminal result = %q, want %q", detail.TerminalResult, tt.wantTerminalResult)
			}
			if detail.FinalCredential != "" {
				t.Fatalf("final credential = %q, want none", detail.FinalCredential)
			}
			if len(detail.Leases) != 3 {
				t.Fatalf("leases = %d, want 3", len(detail.Leases))
			}
			for _, lease := range detail.Leases {
				if lease.Status != tt.wantFinalStatus {
					t.Fatalf("lease %s status = %q, want %q", lease.ResourceType, lease.Status, tt.wantFinalStatus)
				}
			}
		})
	}
}
