package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/httpapi"
	"abyssalauvreleasegate/internal/store"
)

func TestModel_ReusedLeaseOpKeyDifferentMissionScope(t *testing.T) {
	tests := []struct {
		name       string
		leaseOpKey string
	}{
		{
			name:       "second mission does not replay first mission lease acquisition",
			leaseOpKey: "ops-reused-lease-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newTestServer := func(t *testing.T) http.Handler {
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
					func() string { id++; return fmt.Sprintf("M-%d", id) },
				)
				return httpapi.New(svc, "").Handler()
			}

			postJSON := func(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
				t.Helper()
				b, err := json.Marshal(body)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
				return rec.Code, rec.Body.Bytes()
			}

			h := newTestServer(t)

			lockMission := func(t *testing.T, hullID, beaconSlotID string, depthLimitM, trimMinG, trimMaxG int64) domain.Mission {
				t.Helper()
				status, body := postJSON(t, h, "/api/missions", map[string]any{
					"voyage_id":           "VGR-01",
					"auv_hull_id":         hullID,
					"beacon_slot_id":      beaconSlotID,
					"sea_state_revision":  1,
					"return_threshold_wh": 10000,
					"depth_limit_m":       depthLimitM,
					"trim_min_g":          trimMinG,
					"trim_max_g":          trimMaxG,
					"waypoints": []map[string]any{
						{"seq": 1, "latitude_microdeg": -12300000, "longitude_microdeg": 4500000, "target_depth_m": depthLimitM - 500, "max_speed_cm_s": 120, "expected_draw_wh": 2000, "dwell_seconds": 60},
						{"seq": 2, "latitude_microdeg": -12350000, "longitude_microdeg": 4520000, "target_depth_m": depthLimitM, "max_speed_cm_s": 120, "expected_draw_wh": 2500, "dwell_seconds": 60},
					},
				})
				if status != http.StatusCreated {
					t.Fatalf("lock %s/%s status = %d; body=%s", hullID, beaconSlotID, status, body)
				}
				var m domain.Mission
				if err := json.Unmarshal(body, &m); err != nil {
					t.Fatalf("decode locked mission: %v", err)
				}
				return m
			}

			authorizeMission := func(t *testing.T, m domain.Mission) {
				t.Helper()
				base := "/api/missions/" + m.MissionID
				for _, signer := range []struct {
					opKey string
					id    string
					role  domain.SignerRole
				}{
					{"auth-tech-" + m.MissionID, "eng-alice", domain.RoleTechnicalAuthorizer},
					{"auth-safe-" + m.MissionID, "eng-bob", domain.RoleSafetyAuthorizer},
				} {
					status, body := postJSON(t, h, base+"/authorizations", map[string]any{
						"generation":   m.Generation,
						"op_key":       signer.opKey,
						"signer_id":    signer.id,
						"role":         string(signer.role),
						"payload_hash": m.LockedPayloadHash,
					})
					if status != http.StatusOK {
						t.Fatalf("authorize %s status = %d; body=%s", signer.id, status, body)
					}
				}
			}

			first := lockMission(t, "HULL-01", "BEACON-01", 4000, -400, 400)
			authorizeMission(t, first)
			firstBase := "/api/missions/" + first.MissionID

			status, body := postJSON(t, h, firstBase+"/leases/acquire", map[string]any{
				"generation": first.Generation,
				"op_key":     tt.leaseOpKey,
			})
			if status != http.StatusOK {
				t.Fatalf("first lease status = %d; body=%s", status, body)
			}
			var firstLease aggregate.LeaseResult
			if err := json.Unmarshal(body, &firstLease); err != nil {
				t.Fatalf("decode first lease result: %v", err)
			}
			if firstLease.State != domain.StateLeasesHeld || len(firstLease.Leases) != 3 {
				t.Fatalf("first lease result state=%q leases=%d; want leases_held with 3 leases", firstLease.State, len(firstLease.Leases))
			}

			status, body = postJSON(t, h, firstBase+"/finalize", map[string]any{
				"generation": first.Generation,
				"op_key":     "cancel-" + first.MissionID,
				"result":     string(domain.FinalCancelled),
			})
			if status != http.StatusOK {
				t.Fatalf("cancel first mission status = %d; body=%s", status, body)
			}

			second := lockMission(t, "HULL-02", "BEACON-02", 2500, -200, 200)
			authorizeMission(t, second)
			secondBase := "/api/missions/" + second.MissionID

			status, body = postJSON(t, h, secondBase+"/leases/acquire", map[string]any{
				"generation": second.Generation,
				"op_key":     tt.leaseOpKey,
			})
			switch status {
			case http.StatusOK:
				var secondLease aggregate.LeaseResult
				if err := json.Unmarshal(body, &secondLease); err != nil {
					t.Fatalf("decode second lease result: %v", err)
				}
				if secondLease.MissionID != second.MissionID {
					t.Fatalf("second lease mission_id = %q, want %q", secondLease.MissionID, second.MissionID)
				}
				if secondLease.State != domain.StateLeasesHeld || len(secondLease.Leases) != 3 {
					t.Fatalf("reused op_key returned 200 with state=%q leases=%d; want leases_held with 3 leases or a deterministic conflict", secondLease.State, len(secondLease.Leases))
				}
				status, body = postJSON(t, h, secondBase+"/segments/1/simulate", map[string]any{
					"generation":           second.Generation,
					"op_key":               "segment-after-reused-lease",
					"simulator_script_ref": "sim-script-01",
					"adapter_status":       string(domain.AttemptSuccess),
					"verdict":              string(domain.VerdictPass),
				})
				if status != http.StatusOK {
					t.Fatalf("simulate after successful second lease status = %d; body=%s", status, body)
				}
			case http.StatusConflict:
				var e domain.Error
				if err := json.Unmarshal(body, &e); err != nil {
					t.Fatalf("decode second lease conflict: %v", err)
				}
				if e.Code != domain.ErrCodeContentConflict && e.Code != "OPERATION_SCOPE_CONFLICT" {
					t.Fatalf("second lease conflict code = %q, want %q or OPERATION_SCOPE_CONFLICT", e.Code, domain.ErrCodeContentConflict)
				}
			default:
				t.Fatalf("second lease status = %d; body=%s; want 200 with leases or 409 conflict", status, body)
			}
		})
	}
}
