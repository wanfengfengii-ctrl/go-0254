package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"abyssalauvreleasegate/internal/domain"
)

func TestModel_VesselConfirmationAudit(t *testing.T) {
	type vesselStep struct {
		name          string
		status        domain.AttemptStatus
		reasonCode    string
		responseHash  string
		wantHTTP      int
		wantErrorCode string
		wantConfirmed bool
		wantRetry     bool
	}

	tests := []struct {
		name                 string
		steps                []vesselStep
		wantStatuses         []domain.AttemptStatus
		wantVesselConfirmed  bool
		wantRetryAttemptNos  map[int64]bool
		wantSuccessAttemptNo int64
	}{
		{
			name: "ordinary failed vessel replies stay auditable before first success",
			steps: []vesselStep{
				{name: "refused", status: domain.AttemptRefused, reasonCode: "bridge_not_ready", wantHTTP: http.StatusOK, wantRetry: true},
				{name: "timeout", status: domain.AttemptTimeout, reasonCode: "ack_timeout", wantHTTP: http.StatusOK, wantRetry: true},
				{name: "malformed", status: domain.AttemptMalformed, reasonCode: "bad_receipt", wantHTTP: http.StatusOK, wantRetry: true},
				{name: "success", status: domain.AttemptSuccess, responseHash: "receipt-ready", wantHTTP: http.StatusOK, wantConfirmed: true},
			},
			wantStatuses:         []domain.AttemptStatus{domain.AttemptRefused, domain.AttemptTimeout, domain.AttemptMalformed, domain.AttemptSuccess},
			wantVesselConfirmed:  true,
			wantRetryAttemptNos:  map[int64]bool{1: true, 2: true, 3: true},
			wantSuccessAttemptNo: 4,
		},
		{
			name: "contradictory second success returns conflict without second audit row",
			steps: []vesselStep{
				{name: "first success", status: domain.AttemptSuccess, responseHash: "receipt-A", wantHTTP: http.StatusOK, wantConfirmed: true},
				{name: "contradictory success", status: domain.AttemptSuccess, responseHash: "receipt-B", wantHTTP: http.StatusConflict, wantErrorCode: domain.ErrCodeVesselConflict},
			},
			wantStatuses:         []domain.AttemptStatus{domain.AttemptSuccess},
			wantVesselConfirmed:  true,
			wantRetryAttemptNos:  map[int64]bool{},
			wantSuccessAttemptNo: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			var mission domain.Mission
			if err := json.Unmarshal(body, &mission); err != nil {
				t.Fatalf("decode mission: %v", err)
			}
			base := "/api/missions/" + mission.MissionID

			for i, auth := range []struct {
				signer string
				role   domain.SignerRole
			}{
				{signer: "eng-alice", role: domain.RoleTechnicalAuthorizer},
				{signer: "eng-bob", role: domain.RoleSafetyAuthorizer},
			} {
				status, body = postJSON(t, h, base+"/authorizations", map[string]any{
					"generation":   mission.Generation,
					"op_key":       "auth-" + itoa(int64(i+1)),
					"signer_id":    auth.signer,
					"role":         string(auth.role),
					"payload_hash": mission.LockedPayloadHash,
				})
				if status != http.StatusOK {
					t.Fatalf("authorize %s status = %d; body=%s", auth.signer, status, body)
				}
			}

			status, body = postJSON(t, h, base+"/leases/acquire", map[string]any{"generation": mission.Generation, "op_key": "lease-1"})
			if status != http.StatusOK {
				t.Fatalf("lease status = %d; body=%s", status, body)
			}

			status, body = postJSON(t, h, base+"/segments/1/simulate", map[string]any{
				"generation": mission.Generation, "op_key": "seg-1", "simulator_script_ref": "sim-script-01",
				"adapter_status": string(domain.AttemptSuccess), "verdict": string(domain.VerdictPass),
			})
			if status != http.StatusOK {
				t.Fatalf("simulate status = %d; body=%s", status, body)
			}

			status, body = postJSON(t, h, base+"/margins/check", map[string]any{"generation": mission.Generation, "op_key": "margin-1"})
			if status != http.StatusOK {
				t.Fatalf("margins status = %d; body=%s", status, body)
			}

			for i, step := range tt.steps {
				status, body = postJSON(t, h, base+"/vessel-confirmations", map[string]any{
					"generation":     mission.Generation,
					"op_key":         "vessel-" + itoa(int64(i+1)),
					"adapter_status": string(step.status),
					"reason_code":    step.reasonCode,
					"response_hash":  step.responseHash,
				})
				if status != step.wantHTTP {
					t.Fatalf("%s status = %d, want %d; body=%s", step.name, status, step.wantHTTP, body)
				}
				if step.wantErrorCode != "" {
					var envelope domain.Error
					if err := json.Unmarshal(body, &envelope); err != nil {
						t.Fatalf("%s decode error envelope: %v", step.name, err)
					}
					if envelope.Code != step.wantErrorCode {
						t.Fatalf("%s code = %q, want %q", step.name, envelope.Code, step.wantErrorCode)
					}
					continue
				}
				var response struct {
					Confirmed      bool  `json:"confirmed"`
					RetryAfterTick int64 `json:"retry_after_tick"`
				}
				if err := json.Unmarshal(body, &response); err != nil {
					t.Fatalf("%s decode response: %v", step.name, err)
				}
				if response.Confirmed != step.wantConfirmed {
					t.Fatalf("%s confirmed = %v, want %v", step.name, response.Confirmed, step.wantConfirmed)
				}
				if step.wantRetry && response.RetryAfterTick <= 0 {
					t.Fatalf("%s retry_after_tick = %d, want positive retry tick", step.name, response.RetryAfterTick)
				}
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("get status = %d; body=%s", rec.Code, rec.Body.String())
			}
			var detail domain.MissionDetail
			if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
				t.Fatalf("decode detail: %v", err)
			}

			if detail.VesselConfirmed != tt.wantVesselConfirmed {
				t.Fatalf("detail vessel_confirmed = %v, want %v", detail.VesselConfirmed, tt.wantVesselConfirmed)
			}
			if len(detail.AdapterAttempts) != len(tt.wantStatuses) {
				t.Fatalf("adapter attempts = %d, want %d: %+v", len(detail.AdapterAttempts), len(tt.wantStatuses), detail.AdapterAttempts)
			}
			for i, wantStatus := range tt.wantStatuses {
				attempt := detail.AdapterAttempts[i]
				wantAttemptNo := int64(i + 1)
				if attempt.AdapterKind != domain.AdapterVessel {
					t.Fatalf("attempt %d adapter kind = %q, want vessel", wantAttemptNo, attempt.AdapterKind)
				}
				if attempt.AttemptNo != wantAttemptNo {
					t.Fatalf("attempt index %d attempt_no = %d, want %d", i, attempt.AttemptNo, wantAttemptNo)
				}
				if attempt.Status != wantStatus {
					t.Fatalf("attempt %d status = %q, want %q", attempt.AttemptNo, attempt.Status, wantStatus)
				}
				if tt.wantRetryAttemptNos[attempt.AttemptNo] && attempt.RetryAfterTick <= 0 {
					t.Fatalf("attempt %d retry_after_tick = %d, want positive retry tick", attempt.AttemptNo, attempt.RetryAfterTick)
				}
				if attempt.AttemptNo == tt.wantSuccessAttemptNo && attempt.Status == domain.AttemptSuccess && attempt.RetryAfterTick != 0 {
					t.Fatalf("success attempt retry_after_tick = %d, want 0", attempt.RetryAfterTick)
				}
			}
		})
	}
}
