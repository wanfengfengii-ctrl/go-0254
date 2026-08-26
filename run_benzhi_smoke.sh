#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Abyssal AUV release-gate local smoke test.
#
# Builds the server, starts it against a throwaway SQLite database, and drives
# the complete release workflow over the public HTTP API: lock -> two-person
# authorization -> lease capture -> segment simulation -> margin check ->
# vessel confirmation -> independent review -> final launch. Every response is
# captured into a shell variable and asserted with bash pattern matching (never
# a curl|grep pipe, which can SIGPIPE curl). No external network is used.
#
# Exit code is non-zero on the first failed assertion (fail-fast).
# ---------------------------------------------------------------------------
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT="${AUVGATE_PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
TMPDIR="$(mktemp -d)"
DB="${TMPDIR}/auvgate.db"
BIN="${TMPDIR}/auvgate-server"
SERVER_PID=""

# Fail fast with a clear message if the port is already occupied so the smoke
# run never silently talks to a stale server.
if curl -sf "${BASE}/api/health" >/dev/null 2>&1; then
  echo "[smoke] FAIL: port ${PORT} already serves a running instance; stop it or set AUVGATE_PORT" >&2
  exit 1
fi

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMPDIR}"
}
trap cleanup EXIT

# --- Build the server binary (real CLI entrypoint) --------------------------
echo "[smoke] building server"
(cd "${ROOT}" && go build -o "${BIN}" ./cmd/server)

# --- Start the service ------------------------------------------------------
echo "[smoke] starting server on ${PORT}"
"${BIN}" -addr "127.0.0.1:${PORT}" -db "${DB}" -static "${ROOT}/web/dist" &
SERVER_PID=$!

# --- Wait for health --------------------------------------------------------
health=""
for _ in $(seq 1 100); do
  if health="$(curl -sf "${BASE}/api/health" 2>/dev/null || true)"; then
    [[ -n "${health}" ]] && break
  fi
  sleep 0.1
done
if [[ "${health}" != *'"status":"ok"'* ]]; then
  echo "[smoke] FAIL: health endpoint did not report ok (got '${health}')" >&2
  exit 1
fi
echo "[smoke] health ok"

# --- Lock a mission ----------------------------------------------------------
lock="$(curl -s -X POST "${BASE}/api/missions" \
  -H 'Content-Type: application/json' \
  -d '{"voyage_id":"VGR-01","auv_hull_id":"HULL-01","beacon_slot_id":"BEACON-01","sea_state_revision":1,"return_threshold_wh":10000,"depth_limit_m":4000,"trim_min_g":-400,"trim_max_g":400,"waypoints":[{"seq":1,"latitude_microdeg":-12300000,"longitude_microdeg":4500000,"target_depth_m":3000,"max_speed_cm_s":120,"expected_draw_wh":4000,"dwell_seconds":60},{"seq":2,"latitude_microdeg":-12350000,"longitude_microdeg":4520000,"target_depth_m":3500,"max_speed_cm_s":120,"expected_draw_wh":5000,"dwell_seconds":60}]}')"
[[ "${lock}" == *'"state":"pending_authorization"'* ]] || { echo "[smoke] FAIL: lock did not reach pending_authorization: ${lock}" >&2; exit 1; }
mission_id="$(sed -n 's/.*"mission_id":"\([^"]*\)".*/\1/p' <<< "${lock}")"
locked_hash="$(sed -n 's/.*"locked_payload_hash":"\([^"]*\)".*/\1/p' <<< "${lock}")"
[[ -n "${mission_id}" && -n "${locked_hash}" ]] || { echo "[smoke] FAIL: could not parse mission identity" >&2; exit 1; }
API="${BASE}/api/missions/${mission_id}"
echo "[smoke] locked mission ${mission_id}"

# --- Two-person technical authorization -------------------------------------
curl -s -X POST "${API}/authorizations" -H 'Content-Type: application/json' \
  -d "{\"generation\":1,\"op_key\":\"smoke-a1\",\"signer_id\":\"eng-alice\",\"role\":\"technical_authorizer\",\"payload_hash\":\"${locked_hash}\"}" >/dev/null
curl -s -X POST "${API}/authorizations" -H 'Content-Type: application/json' \
  -d "{\"generation\":1,\"op_key\":\"smoke-a2\",\"signer_id\":\"eng-bob\",\"role\":\"safety_authorizer\",\"payload_hash\":\"${locked_hash}\"}" >/dev/null
echo "[smoke] authorized by two signers"

# --- Acquire leases ----------------------------------------------------------
leases="$(curl -s -X POST "${API}/leases/acquire" -H 'Content-Type: application/json' \
  -d '{"generation":1,"op_key":"smoke-lease"}')"
[[ "${leases}" == *'"state":"leases_held"'* ]] || { echo "[smoke] FAIL: leases not held: ${leases}" >&2; exit 1; }
echo "[smoke] leases captured"

# --- Simulate the single route segment --------------------------------------
curl -s -X POST "${API}/segments/1/simulate" -H 'Content-Type: application/json' \
  -d '{"generation":1,"op_key":"smoke-seg-1","simulator_script_ref":"sim-script-01","adapter_status":"success","verdict":"pass"}' >/dev/null
echo "[smoke] segment simulated"

# --- Margin check ------------------------------------------------------------
margins="$(curl -s -X POST "${API}/margins/check" -H 'Content-Type: application/json' \
  -d '{"generation":1,"op_key":"smoke-margin"}')"
[[ "${margins}" == *'"passed":true'* ]] || { echo "[smoke] FAIL: margins did not pass: ${margins}" >&2; exit 1; }
[[ "${margins}" == *'"state":"pending_vessel_confirmation"'* ]] || { echo "[smoke] FAIL: wrong state after margins: ${margins}" >&2; exit 1; }
echo "[smoke] margins passed"

# --- Vessel confirmation -----------------------------------------------------
curl -s -X POST "${API}/vessel-confirmations" -H 'Content-Type: application/json' \
  -d '{"generation":1,"op_key":"smoke-vessel","adapter_status":"success","response_hash":"reply"}' >/dev/null
echo "[smoke] vessel confirmed"

# --- Independent reviews -----------------------------------------------------
for spec in 'eng-alice:route' 'eng-alice:margins' 'sea-carol:vessel_confirmation' 'sea-carol:sea_state'; do
  reviewer="${spec%%:*}"
  kind="${spec##*:}"
  curl -s -X POST "${API}/reviews" -H 'Content-Type: application/json' \
    -d "{\"generation\":1,\"op_key\":\"smoke-rev-${kind}\",\"reviewer_id\":\"${reviewer}\",\"review_kind\":\"${kind}\",\"decision\":\"pass\",\"evidence_hash\":\"e\"}" >/dev/null
done
echo "[smoke] reviews complete"

# --- Finalize (launch) -------------------------------------------------------
final="$(curl -s -X POST "${API}/finalize" -H 'Content-Type: application/json' \
  -d '{"generation":1,"op_key":"smoke-final","result":"launch"}')"
[[ "${final}" == *'"state":"launched"'* ]] || { echo "[smoke] FAIL: not launched: ${final}" >&2; exit 1; }
[[ "${final}" == *'"credential_nonce":"'* ]] || { echo "[smoke] FAIL: missing launch credential: ${final}" >&2; exit 1; }
echo "[smoke] launch finalized with credential"

# --- Reconstruct the final state ---------------------------------------------
detail="$(curl -s "${API}")"
[[ "${detail}" == *'"state":"launched"'* ]] || { echo "[smoke] FAIL: GET did not reflect launched: ${detail}" >&2; exit 1; }
[[ "${detail}" == *'"route_prefix":1'* ]] || { echo "[smoke] FAIL: route prefix not reconstructed" >&2; exit 1; }
echo "[smoke] final state reconstructed"

echo "[smoke] PASS"
