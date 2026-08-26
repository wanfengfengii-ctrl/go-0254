# Abyssal AUV Pre-Launch Track Package and Return-Safety Release Gate

## 项目目标

A single-node Go backend with a connected browser operations console for a marine engineering trial crew deciding one deep-sea AUV launch. The service freezes a vessel voyage, AUV hull identifier, waypoint sequence, depth envelope, energy budget, buoyancy trim, acoustic beacon slot, sea-state window, return threshold, and irreversible mission generation. It then drives a closed workflow through two-person technical authorization, hull and beacon-slot lease capture, ordered segment simulation, integer margin checks, vessel adapter confirmation, independent review, and one final outcome: launch permission, engineering isolation, or mission cancellation. SQLite WAL persistence records aggregate state, lease ledgers, idempotency decisions, external-call attempts, evidence chains, review results, and final credentials so restart recovery is deterministic.

## 端到端业务流程

1. Freeze mission generation: create a draft, validate catalog references, normalize waypoints by sequence number, compute immutable hashes for route package and sea-state revision, then move from pending lock to pending authorization.
2. Authorize technically: accept exactly two distinct qualified signers for the locked generation; reject overlapping roles, stale generation references, or content changes under the same operation key.
3. Acquire leases atomically: reserve the AUV hull, acoustic beacon slot, and route-package generation for the open mission; competing attempts receive deterministic denial reasons sorted by voyage, hull, and waypoint sequence.
4. Verify route prefix: submit segment simulation evidence strictly in waypoint sequence. Accepted segments extend the checked prefix by one; unknown, repeated, skipped, stale, or simulator-failed segments leave the prefix unchanged and produce auditable retry records.
5. Check return safety margins: calculate depth, velocity, energy, return reserve, and buoyancy trim with bounded integer arithmetic; only evidence passing all locked thresholds can advance the mission.
6. Confirm vessel readiness: record adapter confirmation attempts from the vessel interface, preserving refusals, disconnects, timeouts, malformed replies, and later retries without fabricating success evidence; contradictory confirmations are rejected deterministically.

## 核心组件与职责

1. AUV and Sea-State Rule Catalog: immutable in-memory seed catalog plus SQLite snapshots for vessel voyages, AUV hull capabilities, trim limits, beacon slots, sea-state windows, reviewer qualifications, and adapter endpoints.
2. Deployment Mission Aggregate: owns state transitions, irreversible generation barrier, terminal-state fence, idempotency keys, sorted rejection reasons, audit events, and restart reconstruction.
3. Hull, Beacon, and Route-Generation Lease Ledger: provides one-time effective leases with transactional acquisition, release only on terminal outcomes, and conflict detection for concurrent starts or slot changes.
4. Route Segment and Margin Evidence Ledger: stores append-only evidence versions for simulator calls, segment-prefix advancement, integer margin calculations, sea-state generation review, and retry scheduling.
5. Vessel Confirmation and Final Arbiter: records vessel adapter attempts, independent review evidence, launch credential creation, engineering isolation, cancellation, and final-result race arbitration.
6. Go HTTP API: JSON endpoints for operators and tests, deterministic error envelopes, logical-clock injection in test mode, and static serving of the connected browser console built from package.json and a lockfile.

## 领域规则与不变量

1. Mission states are pending_lock, pending_authorization, leases_held, route_checking, margin_checking, pending_vessel_confirmation, launchable, launched, engineering_isolation, and cancelled; terminal states reject all later ordinary mutations.
2. A locked mission generation is immutable: vessel voyage, AUV hull, waypoint list, depth envelope, energy budget, buoyancy trim, beacon slot, sea-state window, return threshold, and hashes cannot be changed in place.
3. AUV hull, beacon slot, and route-package generation can each have only one effective lease across non-terminal missions; acquisition occurs in one SQLite transaction and leaves no partial rows on failure.
4. Route verification advances only as a contiguous prefix from waypoint 1 through N-1 segments. Duplicate, skipped, unknown, or stale segment submissions never change the prefix.
5. All numeric safety checks use signed 64-bit integer rules with explicit bounds for waypoint count, depth, speed, energy draw, return reserve, trim mass, and computed products; overflow is a stable validation failure.
6. Evidence chains are append-only by evidence kind and generation. New sea-state revisions create new review evidence for the current generation; late replies for older generations remain recorded but cannot affect current conclusions.
7. The two technical authorizers and independent reviewers must be distinct where required and must match catalog qualifications for AUV release operations.
8. Only one final result may be committed. Launch credential creation, engineering isolation, and cancellation compete through a final-state compare-and-set guarded by the aggregate version.

## 数据模型与持久化

1. missions: mission_id, voyage_id, auv_hull_id, generation, state, locked_payload_hash, route_hash, sea_state_revision, return_threshold_wh, depth_limit_m, trim_bounds_g, aggregate_version, terminal_result, created_at_tick, updated_at_tick.
2. waypoints: mission_id, generation, seq, latitude_microdeg, longitude_microdeg, target_depth_m, max_speed_cm_s, expected_draw_wh, dwell_seconds, checksum.
3. leases: lease_id, resource_type, resource_key, mission_id, generation, status, acquired_at_tick, released_at_tick, unique effective index on resource_type/resource_key/status=open.
4. authorizations: mission_id, generation, signer_id, role, payload_hash, op_key, accepted_at_tick, unique signer and role constraints per generation.
5. segment_evidence: evidence_id, mission_id, generation, segment_seq, attempt_no, simulator_script_ref, request_hash, response_hash, verdict, reason_code, created_at_tick, immutable predecessor pointer.
6. margin_evidence: evidence_id, mission_id, generation, route_prefix, energy_used_wh, return_reserve_wh, peak_depth_m, peak_speed_cm_s, trim_delta_g, verdict, reason_code, created_at_tick.
7. adapter_attempts: attempt_id, mission_id, generation, adapter_kind, request_hash, status, reason_code, retry_after_tick, attempt_no, created_at_tick.
8. reviews_and_final: review_id or final_id, mission_id, generation, reviewer_id, review_kind, evidence_hash, decision, credential_nonce, committed_at_tick.
9. idempotency_records: op_key, operation_kind, mission_id, generation, request_hash, response_hash, status_code, stable_error_code, created_at_tick.

## 公开接口

1. POST /api/missions locks a new mission generation after catalog and integer-bound validation, returning mission_id, generation, state, and hashes.
2. POST /api/missions/{id}/authorizations records a technical authorization with op_key idempotency and stable errors for duplicate signer, role overlap, or payload conflict.
3. POST /api/missions/{id}/leases/acquire atomically captures hull, beacon slot, and route-generation leases, with deterministic conflict reporting.
4. POST /api/missions/{id}/segments/{seq}/simulate submits or retries simulator evidence for one segment and returns prefix_before, prefix_after, attempt status, and sorted reasons.
5. POST /api/missions/{id}/margins/check runs deterministic energy, depth, speed, return-reserve, and buoyancy arithmetic for the current prefix or complete route.
6. POST /api/missions/{id}/vessel-confirmations records vessel adapter confirmation attempts and retry metadata without advancing on refusal, timeout, disconnect, or malformed response.
7. POST /api/missions/{id}/reviews records independent review evidence for route, margins, vessel confirmation, and sea-state generation.
8. POST /api/missions/{id}/finalize commits launch, engineering isolation, or cancellation through a single final arbiter and returns the unique credential only for successful launch.
9. GET /api/missions/{id} returns the reconstructed aggregate, leases, prefix, evidence heads, retry queue, review status, and final result for the browser operations console.
10. Browser console: Vite-based static client with deterministic build lockfile, showing the active mission, state machine, lease status, prefix progress, evidence versions, retry attempts, and final action controls backed by the Go API.

## 失败边界

1. Every mutating request runs inside one SQLite transaction; failures roll back leases, prefix advancement, margin evidence, adapter success, final credentials, and idempotency writes together.
2. Stable error envelope: code, operation, mission_id, generation, state, reasons sorted by voyage_id, auv_hull_id, waypoint_seq, and resource_key where applicable.
3. External simulator and vessel adapter calls are represented by attempt rows before result interpretation; refusal, disconnect, timeout, and malformed payload create retryable audit records only.
4. Operation keys are idempotent for byte-identical request bodies and reject divergent bodies with OPERATION_CONTENT_CONFLICT without changing mission state.
5. Restart recovery rebuilds mission state from SQLite rows, validates open leases against non-terminal missions, restores retry schedules from logical ticks, and preserves evidence-chain heads.
6. Late callbacks after generation replacement or terminal finalization append only to their attempt history when addressable and never advance prefix, confirmation, review, or final state.
7. Concurrent finalization uses aggregate_version compare-and-set; one winner commits, all losing transactions return FINAL_ALREADY_COMMITTED with the committed result.
8. Arithmetic validation rejects overflow, negative durations, excessive waypoint count, depth beyond envelope, speed beyond hull capability, energy deficit, and trim outside bounds before evidence can be marked effective.

## 验收标准

1. Locking freezes vessel voyage, AUV hull, waypoint sequence, depth envelope, energy budget, buoyancy parameters, beacon slot, sea-state window, return threshold, and mission generation with immutable hashes.
2. Each AUV hull, beacon slot, and route-package generation has at most one effective lease among non-terminal missions; concurrent authorization, slot change, and start attempts are atomically adjudicated.
3. Route segments verify only in locked sequence order; unknown, stale, repeated, or skipped sequence submissions do not advance the prefix, while same op_key plus same content is idempotent and conflicting content is rejected.
4. Energy, depth, speed, return-reserve, and buoyancy calculations use deterministic integer arithmetic with explicit boundary and overflow checks; failing checks write no effective success evidence.
5. Simulator and vessel adapter refusal, disconnect, timeout, and malformed response produce auditable retry attempts with deterministic retry target, count, and trigger tick, never synthetic success.
6. Sea-state changes create new generation-scoped review evidence while older late replies remain immutable and cannot affect the current conclusion.
7. When route prefix is complete, margins meet thresholds, vessel confirmation is closed, and two distinct qualified reviewers approve, only the successful final competitor can create the unique launch credential.
8. Competing launch, engineering isolation, and cancellation requests produce exactly one terminal result; all later segment, confirmation, review, and ordinary mutation attempts are rejected without state change.

## 确定性测试场景

1. mission_lock_test.go: locks a valid deep-sea mission; rejects hull-voyage mismatch; rejects stale sea-state revision; verifies immutable hashes and generation barrier.
2. lease_concurrency_test.go: uses goroutine barriers to race two missions for the same AUV hull and beacon slot; asserts exactly one effective lease set and no partial lease rows for losers.
3. segment_prefix_test.go: accepts segments 1..N in sequence; rejects duplicate sequence, skipped sequence, unknown sequence, stale generation, and op_key content conflict while preserving prefix.
4. margin_rules_test.go: covers exact threshold pass, one-unit energy deficit, trim high/low boundary, depth envelope breach, speed breach, waypoint-length cap, and 64-bit overflow rejection.
5. adapter_retry_test.go: scripts simulator refusal, disconnect, timeout, and malformed vessel reply; asserts retry rows, attempt counts, logical ticks, and no false prefix or confirmation success.
6. generation_isolation_test.go: records sea-state revision replacement and late callback for an older generation; asserts append-only evidence history and unchanged current conclusion.
7. final_arbiter_test.go: races launch, engineering isolation, and cancellation after all prerequisites; asserts one terminal result, unique credential when launch wins, and deterministic loser errors.
8. restart_recovery_test.go: restarts the service over the same SQLite file after leases, partial prefix, retry attempts, and reviews; asserts reconstructed state and continued deterministic behavior.

## 组件追踪关系

1. AUV and Sea-State Rule Catalog covers lock validation, voyage-hull fit, sea-state freshness, integer capability limits, and reviewer qualification checks.
2. Deployment Mission Aggregate covers states, generation barrier, idempotency, sorted reasons, terminal fence, and restart reconstruction.
3. Hull, Beacon, and Route-Generation Lease Ledger covers exclusive resource capture, concurrent contention, atomic slot decisions, and no partial lease persistence.
4. Route Segment and Margin Evidence Ledger covers ordered prefix semantics, append-only evidence versions, simulator attempt audit, deterministic arithmetic, and sea-state generation isolation.
5. Vessel Confirmation and Final Arbiter covers vessel adapter attempts, contradictory confirmation handling, independent review closure, final result race, and unique launch credential issuance.
6. Go HTTP API covers deterministic JSON contracts, browser console connectivity, logical-clock test controls, static asset serving, and public-test orchestration.

## 独特性

The plan centers on deep-sea AUV launch readiness: immutable mission generations, exclusive hull and acoustic-slot use, ordered waypoint simulation, return-safety arithmetic, vessel adapter closure, and final marine release arbitration. Its state, evidence, and concurrency problems arise from aerospace-marine trial operations rather than generic administrative records.
