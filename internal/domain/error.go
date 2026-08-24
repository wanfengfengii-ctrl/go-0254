package domain

import (
	"fmt"
	"sort"
)

// Stable error codes returned in the deterministic error envelope.
const (
	ErrCodeValidation            = "VALIDATION_FAILED"
	ErrCodeNotFound              = "MISSION_NOT_FOUND"
	ErrCodeTerminal              = "MISSION_TERMINAL"
	ErrCodeContentConflict       = "OPERATION_CONTENT_CONFLICT"
	ErrCodeDuplicateSigner       = "DUPLICATE_SIGNER"
	ErrCodeRoleOverlap           = "ROLE_OVERLAP"
	ErrCodeStaleGeneration       = "STALE_GENERATION"
	ErrCodeLeaseConflict         = "LEASE_CONFLICT"
	ErrCodeSegmentOrder          = "SEGMENT_ORDER_VIOLATION"
	ErrCodeMarginFailed          = "MARGIN_CHECK_FAILED"
	ErrCodeOverflow              = "INTEGER_OVERFLOW"
	ErrCodeFinalAlreadyCommitted = "FINAL_ALREADY_COMMITTED"
	ErrCodeInternal              = "INTERNAL_ERROR"
	ErrCodePayloadConflict       = "PAYLOAD_CONFLICT"
	ErrCodeNotAuthorized         = "NOT_AUTHORIZED"
	ErrCodeReviewIncomplete      = "REVIEW_INCOMPLETE"
	ErrCodePrerequisiteMissing   = "PREREQUISITE_MISSING"
	ErrCodeVesselConflict        = "CONTRADICTORY_CONFIRMATION"
	ErrCodeUnqualified           = "UNQUALIFIED_REVIEWER"
	ErrCodeStateMachine          = "STATE_TRANSITION_INVALID"
	ErrCodeUnknownResult         = "UNKNOWN_FINAL_RESULT"
)

// Error is the stable, deterministic error envelope returned by every API.
type Error struct {
	Code       string   `json:"code"`
	Operation  string   `json:"operation"`
	MissionID  string   `json:"mission_id,omitempty"`
	Generation int64    `json:"generation,omitempty"`
	State      State    `json:"state,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (%s) mission=%s gen=%d", e.Code, e.Operation, e.MissionID, e.Generation)
}

// NewError builds a stable error envelope.
func NewError(code, operation string) *Error {
	return &Error{Code: code, Operation: operation}
}

// WithMission annotates the envelope with aggregate identity.
func (e *Error) WithMission(missionID string, generation int64, state State) *Error {
	e.MissionID = missionID
	e.Generation = generation
	e.State = state
	return e
}

// WithReasons attaches sorted rejection reasons.
func (e *Error) WithReasons(reasons ...string) *Error {
	e.Reasons = append(e.Reasons, reasons...)
	return e
}

// SortedReasons returns a deterministically sorted copy of the supplied
// rejection reasons. The plan requires reasons to be sorted by voyage_id,
// auv_hull_id, waypoint_seq, and resource_key where applicable; a lexical sort
// of the stable reason tokens preserves that contract across every envelope.
func SortedReasons(reasons ...string) []string {
	out := make([]string, len(reasons))
	copy(out, reasons)
	sort.Strings(out)
	return out
}
