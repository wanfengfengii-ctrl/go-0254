// Package store defines the persistence boundary for the deployment mission
// system and provides a SQLite WAL-backed implementation. The boundary keeps
// every mutating aggregate operation inside a single SQLite transaction so that
// leases, prefix advancement, margin evidence, adapter attempts, final
// credentials, and idempotency writes either commit together or roll back
// together.
package store

import (
	"context"
	"errors"

	"abyssalauvreleasegate/internal/domain"
)

// ErrNotFound is returned when a referenced aggregate does not exist.
var ErrNotFound = errors.New("store: mission not found")

// ErrVersionConflict is returned when an aggregate compare-and-set update
// observes a stale aggregate_version, meaning a concurrent mutation won.
var ErrVersionConflict = errors.New("store: aggregate version conflict")

// Store is the persistence boundary used by the aggregate and HTTP layers.
type Store interface {
	// Close releases the underlying database handle.
	Close() error

	// WithTx runs fn inside a single database transaction. If fn returns an
	// error the transaction is rolled back; otherwise it is committed.
	WithTx(ctx context.Context, fn func(Tx) error) error

	// CreateMission atomically persists a new locked mission generation with
	// its ordered waypoints. It is retained as a direct method for tests and
	// bootstrap callers that do not need an explicit transaction boundary.
	CreateMission(ctx context.Context, m domain.Mission, waypoints []domain.Waypoint) error

	// GetMission returns a mission aggregate and its ordered waypoints.
	GetMission(ctx context.Context, missionID string) (domain.Mission, []domain.Waypoint, error)

	// GetAuthorizations returns the technical authorization records for a
	// generation ordered by acceptance tick.
	GetAuthorizations(ctx context.Context, missionID string, generation int64) ([]domain.Authorization, error)

	// GetLeases returns every lease row for a mission ordered by resource type.
	GetLeases(ctx context.Context, missionID string) ([]domain.Lease, error)

	// GetSegmentEvidence returns append-only segment evidence ordered by
	// segment sequence then attempt number.
	GetSegmentEvidence(ctx context.Context, missionID string, generation int64) ([]domain.SegmentEvidence, error)

	// GetMarginEvidence returns append-only margin evidence ordered by tick.
	GetMarginEvidence(ctx context.Context, missionID string, generation int64) ([]domain.MarginEvidence, error)

	// GetAdapterAttempts returns adapter attempt rows ordered by attempt number.
	GetAdapterAttempts(ctx context.Context, missionID string, generation int64) ([]domain.AdapterAttempt, error)

	// GetReviews returns independent review rows ordered by commit tick.
	GetReviews(ctx context.Context, missionID string, generation int64) ([]domain.Review, error)

	// GetFinal returns the committed final outcome for a generation, if any.
	GetFinal(ctx context.Context, missionID string, generation int64) (domain.FinalOutcome, bool, error)

	// GetIdempotency returns a previously recorded operation-key outcome.
	GetIdempotency(ctx context.Context, opKey, operationKind string) (domain.IdempotencyRecord, bool, error)
}

// Tx is the transaction-scoped surface exposed to the aggregate inside WithTx.
// It offers both reads (for read-modify-write correctness) and writes.
type Tx interface {
	// GetMission returns a mission and its ordered waypoints.
	GetMission(ctx context.Context, missionID string) (domain.Mission, []domain.Waypoint, error)

	// UpdateMission applies a compare-and-set update to a mission row: the
	// update only takes effect when the stored aggregate_version equals
	// expectedVersion, and on success the version is incremented.
	UpdateMission(ctx context.Context, m domain.Mission, expectedVersion int64) error

	// InsertAuthorization appends a technical authorization record.
	InsertAuthorization(ctx context.Context, a domain.Authorization) error
	// GetAuthorizations reads technical authorization records for a generation.
	GetAuthorizations(ctx context.Context, missionID string, generation int64) ([]domain.Authorization, error)

	// InsertLeases inserts one or more lease rows in this transaction. The
	// effective-open unique index enforces at-most-one open lease per resource.
	InsertLeases(ctx context.Context, leases []domain.Lease) error
	// GetLeases reads lease rows for a mission.
	GetLeases(ctx context.Context, missionID string) ([]domain.Lease, error)
	// FindOpenLeases returns currently open leases for a resource.
	FindOpenLeases(ctx context.Context, resourceType, resourceKey string) ([]domain.Lease, error)
	// ReleaseLeases marks every open lease for a mission as released.
	ReleaseLeases(ctx context.Context, missionID string, tick int64) error

	// InsertSegmentEvidence appends a segment simulation evidence row.
	InsertSegmentEvidence(ctx context.Context, ev domain.SegmentEvidence) error
	// GetSegmentEvidence reads segment evidence ordered by sequence and attempt.
	GetSegmentEvidence(ctx context.Context, missionID string, generation int64) ([]domain.SegmentEvidence, error)

	// InsertMarginEvidence appends a margin-check evidence row.
	InsertMarginEvidence(ctx context.Context, ev domain.MarginEvidence) error
	// GetMarginEvidence reads margin evidence ordered by tick.
	GetMarginEvidence(ctx context.Context, missionID string, generation int64) ([]domain.MarginEvidence, error)

	// InsertAdapterAttempt appends a vessel/simulator adapter attempt row.
	InsertAdapterAttempt(ctx context.Context, a domain.AdapterAttempt) error
	// GetAdapterAttempts reads adapter attempts ordered by attempt number.
	GetAdapterAttempts(ctx context.Context, missionID string, generation int64) ([]domain.AdapterAttempt, error)

	// InsertReview appends an independent review row.
	InsertReview(ctx context.Context, r domain.Review) error
	// GetReviews reads review rows ordered by commit tick.
	GetReviews(ctx context.Context, missionID string, generation int64) ([]domain.Review, error)

	// InsertFinal records the single committed terminal outcome and, for a
	// successful launch, the unique credential nonce.
	InsertFinal(ctx context.Context, missionID string, generation int64, result domain.FinalResult, nonce string, tick int64) error
	// GetFinal reads the committed final outcome for a generation.
	GetFinal(ctx context.Context, missionID string, generation int64) (domain.FinalOutcome, bool, error)

	// GetIdempotency reads a previously recorded operation-key outcome.
	GetIdempotency(ctx context.Context, opKey, operationKind string) (domain.IdempotencyRecord, bool, error)
	// InsertIdempotency records an operation-key outcome.
	InsertIdempotency(ctx context.Context, rec domain.IdempotencyRecord) error
}
