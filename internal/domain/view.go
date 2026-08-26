package domain

// MissionDetail is the fully reconstructed aggregate returned to the browser
// operations console. It flattens the mission fields and appends the lease
// ledger, technical authorizations, evidence chains, adapter attempts, review
// status, and final result so a single GET reflects all persisted state.
type MissionDetail struct {
	Mission

	Leases          []Lease           `json:"leases"`
	Authorizations  []Authorization   `json:"authorizations"`
	SegmentEvidence []SegmentEvidence `json:"segment_evidence"`
	MarginEvidence  []MarginEvidence  `json:"margin_evidence"`
	AdapterAttempts []AdapterAttempt  `json:"adapter_attempts"`
	Reviews         []Review          `json:"reviews"`
	RoutePrefix     int64             `json:"route_prefix"`
	VesselConfirmed bool              `json:"vessel_confirmed"`
	ReviewComplete  bool              `json:"review_complete"`
	FinalCredential string            `json:"final_credential,omitempty"`
}

// FinalOutcome is the single committed terminal result with, for a successful
// launch, the unique one-time credential issued by the final arbiter.
type FinalOutcome struct {
	MissionID       string      `json:"mission_id"`
	Generation      int64       `json:"generation"`
	State           State       `json:"state"`
	TerminalResult  FinalResult `json:"terminal_result"`
	CredentialNonce string      `json:"credential_nonce,omitempty"`
}
