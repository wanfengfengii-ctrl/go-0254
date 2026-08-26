package domain

// Verdict is the outcome recorded for a piece of evidence.
type Verdict string

const (
	VerdictPass  Verdict = "pass"
	VerdictFail  Verdict = "fail"
	VerdictRetry Verdict = "retry"
	VerdictStale Verdict = "stale"
)

// AdapterKind identifies an external integration whose attempts are audited.
type AdapterKind string

const (
	AdapterSimulator AdapterKind = "simulator"
	AdapterVessel    AdapterKind = "vessel"
)

// AttemptStatus describes how an external call resolved before interpretation.
type AttemptStatus string

const (
	AttemptSuccess    AttemptStatus = "success"
	AttemptRefused    AttemptStatus = "refused"
	AttemptDisconnect AttemptStatus = "disconnect"
	AttemptTimeout    AttemptStatus = "timeout"
	AttemptMalformed  AttemptStatus = "malformed"
)

// SegmentEvidence is an append-only record of one segment simulation attempt.
type SegmentEvidence struct {
	EvidenceID         string  `json:"evidence_id"`
	MissionID          string  `json:"mission_id"`
	Generation         int64   `json:"generation"`
	SegmentSeq         int64   `json:"segment_seq"`
	AttemptNo          int64   `json:"attempt_no"`
	SimulatorScriptRef string  `json:"simulator_script_ref"`
	RequestHash        string  `json:"request_hash"`
	ResponseHash       string  `json:"response_hash"`
	Verdict            Verdict `json:"verdict"`
	ReasonCode         string  `json:"reason_code"`
	CreatedAtTick      int64   `json:"created_at_tick"`
	PredecessorID      string  `json:"predecessor_id"`
}

// MarginEvidence is an append-only record of one integer margin check.
type MarginEvidence struct {
	EvidenceID      string  `json:"evidence_id"`
	MissionID       string  `json:"mission_id"`
	Generation      int64   `json:"generation"`
	RoutePrefix     int64   `json:"route_prefix"`
	EnergyUsedWh    int64   `json:"energy_used_wh"`
	ReturnReserveWh int64   `json:"return_reserve_wh"`
	PeakDepthM      int64   `json:"peak_depth_m"`
	PeakSpeedCmS    int64   `json:"peak_speed_cm_s"`
	TrimDeltaG      int64   `json:"trim_delta_g"`
	Verdict         Verdict `json:"verdict"`
	ReasonCode      string  `json:"reason_code"`
	CreatedAtTick   int64   `json:"created_at_tick"`
}

// AdapterAttempt is an append-only record of an external adapter call.
type AdapterAttempt struct {
	AttemptID      string        `json:"attempt_id"`
	MissionID      string        `json:"mission_id"`
	Generation     int64         `json:"generation"`
	AdapterKind    AdapterKind   `json:"adapter_kind"`
	RequestHash    string        `json:"request_hash"`
	Status         AttemptStatus `json:"status"`
	ReasonCode     string        `json:"reason_code"`
	RetryAfterTick int64         `json:"retry_after_tick"`
	AttemptNo      int64         `json:"attempt_no"`
	CreatedAtTick  int64         `json:"created_at_tick"`
}

// ReviewKind identifies what an independent reviewer approves.
type ReviewKind string

const (
	ReviewRoute              ReviewKind = "route"
	ReviewMargins            ReviewKind = "margins"
	ReviewVesselConfirmation ReviewKind = "vessel_confirmation"
	ReviewSeaState           ReviewKind = "sea_state"
)

// Review is an independent review decision for one generation.
type Review struct {
	ReviewID        string     `json:"review_id"`
	MissionID       string     `json:"mission_id"`
	Generation      int64      `json:"generation"`
	ReviewerID      string     `json:"reviewer_id"`
	ReviewKind      ReviewKind `json:"review_kind"`
	EvidenceHash    string     `json:"evidence_hash"`
	Decision        Verdict    `json:"decision"`
	CredentialNonce string     `json:"credential_nonce,omitempty"`
	CommittedAtTick int64      `json:"committed_at_tick"`
}

// FinalResult is the single terminal outcome committed by the final arbiter.
type FinalResult string

const (
	FinalLaunch    FinalResult = "launch"
	FinalIsolation FinalResult = "engineering_isolation"
	FinalCancelled FinalResult = "cancelled"
)
