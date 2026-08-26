package domain

// SignerRole is the qualification a signer must hold for a given operation.
type SignerRole string

const (
	RoleTechnicalAuthorizer SignerRole = "technical_authorizer"
	RoleSafetyAuthorizer    SignerRole = "safety_authorizer"
	RoleIndependentReviewer SignerRole = "independent_reviewer"
)

// TechnicalAuthorizationRoles returns the two distinct roles that together
// satisfy the two-person technical authorization gate. A locked generation is
// authorized only when one qualified signer holds each role and the two signers
// are distinct people.
func TechnicalAuthorizationRoles() []SignerRole {
	return []SignerRole{RoleTechnicalAuthorizer, RoleSafetyAuthorizer}
}

// Authorization is a technical authorization record for a locked generation.
type Authorization struct {
	MissionID      string     `json:"mission_id"`
	Generation     int64      `json:"generation"`
	SignerID       string     `json:"signer_id"`
	Role           SignerRole `json:"role"`
	PayloadHash    string     `json:"payload_hash"`
	OpKey          string     `json:"op_key"`
	AcceptedAtTick int64      `json:"accepted_at_tick"`
}

// IdempotencyRecord stores the outcome of a previously executed operation key.
type IdempotencyRecord struct {
	OpKey           string `json:"op_key"`
	OperationKind   string `json:"operation_kind"`
	MissionID       string `json:"mission_id"`
	Generation      int64  `json:"generation"`
	RequestHash     string `json:"request_hash"`
	ResponseHash    string `json:"response_hash"`
	StatusCode      int    `json:"status_code"`
	StableErrorCode string `json:"stable_error_code,omitempty"`
	CreatedAtTick   int64  `json:"created_at_tick"`
}
