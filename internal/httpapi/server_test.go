package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/httpapi"
	"abyssalauvreleasegate/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
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
	return httpapi.New(svc, "").Handler()
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestHealthEndpoint(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLockAndGetMissionHTTP(t *testing.T) {
	h := newTestServer(t)

	body := []byte(`{
		"voyage_id":"VGR-01",
		"auv_hull_id":"HULL-01",
		"beacon_slot_id":"BEACON-01",
		"sea_state_revision":1,
		"return_threshold_wh":10000,
		"depth_limit_m":4000,
		"trim_min_g":-400,
		"trim_max_g":400,
		"waypoints":[
			{"seq":1,"latitude_microdeg":-12300000,"longitude_microdeg":4500000,"target_depth_m":3000,"max_speed_cm_s":120,"expected_draw_wh":4000,"dwell_seconds":60},
			{"seq":2,"latitude_microdeg":-12350000,"longitude_microdeg":4520000,"target_depth_m":3500,"max_speed_cm_s":120,"expected_draw_wh":5000,"dwell_seconds":60}
		]
	}`)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/missions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("lock status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	var m domain.Mission
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode lock response: %v", err)
	}
	if m.MissionID == "" || m.State != domain.StatePendingAuthorization {
		t.Fatalf("unexpected mission: %+v", m)
	}

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/missions/"+m.MissionID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", get.Code, get.Body.String())
	}
	var got domain.Mission
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.MissionID != m.MissionID {
		t.Errorf("mission id mismatch")
	}
}

func TestLockValidationErrorEnvelope(t *testing.T) {
	h := newTestServer(t)
	body := []byte(`{"voyage_id":"VGR-01","auv_hull_id":"HULL-01","beacon_slot_id":"BEACON-01","sea_state_revision":99,"waypoints":[{"seq":1,"target_depth_m":100,"max_speed_cm_s":10,"expected_draw_wh":1,"dwell_seconds":1}]}`)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/missions", bytes.NewReader(body)))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var e domain.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if e.Code != domain.ErrCodeValidation {
		t.Errorf("code = %q, want validation", e.Code)
	}
	if len(e.Reasons) == 0 {
		t.Error("expected at least one rejection reason")
	}
}

func TestGetMissingMissionHTTP(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/missions/NOPE", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
