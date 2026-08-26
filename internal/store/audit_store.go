package store

import (
	"context"
	"database/sql"
	"fmt"

	"abyssalauvreleasegate/internal/domain"
)

// GetAuthorizations returns technical authorization records for a generation
// ordered by acceptance tick.
func (s *SQLite) GetAuthorizations(ctx context.Context, missionID string, generation int64) ([]domain.Authorization, error) {
	return authorizationsQ(ctx, s.db, missionID, generation)
}

func authorizationsQ(ctx context.Context, q queryer, missionID string, generation int64) ([]domain.Authorization, error) {
	rows, err := q.QueryContext(ctx, `SELECT
		mission_id, generation, signer_id, role, payload_hash, op_key, accepted_at_tick
		FROM authorizations WHERE mission_id = ? AND generation = ?
		ORDER BY accepted_at_tick ASC`, missionID, generation)
	if err != nil {
		return nil, fmt.Errorf("query authorizations: %w", err)
	}
	defer rows.Close()
	var out []domain.Authorization
	for rows.Next() {
		var a domain.Authorization
		if err := rows.Scan(
			&a.MissionID, &a.Generation, &a.SignerID, &a.Role, &a.PayloadHash,
			&a.OpKey, &a.AcceptedAtTick,
		); err != nil {
			return nil, fmt.Errorf("scan authorization: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// InsertAuthorization appends a technical authorization record. The unique
// signer and role constraints are enforced at the database layer.
func (t *sqliteTx) InsertAuthorization(ctx context.Context, a domain.Authorization) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO authorizations (
		mission_id, generation, signer_id, role, payload_hash, op_key, accepted_at_tick
	) VALUES (?,?,?,?,?,?,?)`,
		a.MissionID, a.Generation, a.SignerID, string(a.Role), a.PayloadHash, a.OpKey, a.AcceptedAtTick,
	)
	if err != nil {
		return fmt.Errorf("insert authorization: %w", err)
	}
	return nil
}

// GetAuthorizations reads authorizations inside the transaction.
func (t *sqliteTx) GetAuthorizations(ctx context.Context, missionID string, generation int64) ([]domain.Authorization, error) {
	return authorizationsQ(ctx, t.tx, missionID, generation)
}

// GetAdapterAttempts returns adapter attempt rows ordered by attempt number.
func (s *SQLite) GetAdapterAttempts(ctx context.Context, missionID string, generation int64) ([]domain.AdapterAttempt, error) {
	return adapterAttemptsQ(ctx, s.db, missionID, generation)
}

func adapterAttemptsQ(ctx context.Context, q queryer, missionID string, generation int64) ([]domain.AdapterAttempt, error) {
	rows, err := q.QueryContext(ctx, `SELECT
		attempt_id, mission_id, generation, adapter_kind, request_hash, status,
		reason_code, retry_after_tick, attempt_no, created_at_tick
		FROM adapter_attempts WHERE mission_id = ? AND generation = ?
		ORDER BY attempt_no ASC`, missionID, generation)
	if err != nil {
		return nil, fmt.Errorf("query adapter attempts: %w", err)
	}
	defer rows.Close()
	var out []domain.AdapterAttempt
	for rows.Next() {
		var a domain.AdapterAttempt
		if err := rows.Scan(
			&a.AttemptID, &a.MissionID, &a.Generation, &a.AdapterKind, &a.RequestHash, &a.Status,
			&a.ReasonCode, &a.RetryAfterTick, &a.AttemptNo, &a.CreatedAtTick,
		); err != nil {
			return nil, fmt.Errorf("scan adapter attempt: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// InsertAdapterAttempt appends a vessel/simulator adapter attempt row.
func (t *sqliteTx) InsertAdapterAttempt(ctx context.Context, a domain.AdapterAttempt) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO adapter_attempts (
		attempt_id, mission_id, generation, adapter_kind, request_hash, status,
		reason_code, retry_after_tick, attempt_no, created_at_tick
	) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.AttemptID, a.MissionID, a.Generation, string(a.AdapterKind), a.RequestHash, string(a.Status),
		a.ReasonCode, a.RetryAfterTick, a.AttemptNo, a.CreatedAtTick,
	)
	if err != nil {
		return fmt.Errorf("insert adapter attempt: %w", err)
	}
	return nil
}

// GetAdapterAttempts reads adapter attempts inside the transaction.
func (t *sqliteTx) GetAdapterAttempts(ctx context.Context, missionID string, generation int64) ([]domain.AdapterAttempt, error) {
	return adapterAttemptsQ(ctx, t.tx, missionID, generation)
}

// GetReviews returns independent review rows ordered by commit tick.
func (s *SQLite) GetReviews(ctx context.Context, missionID string, generation int64) ([]domain.Review, error) {
	return reviewsQ(ctx, s.db, missionID, generation)
}

func reviewsQ(ctx context.Context, q queryer, missionID string, generation int64) ([]domain.Review, error) {
	rows, err := q.QueryContext(ctx, `SELECT
		id, mission_id, generation, reviewer_id, review_kind, evidence_hash,
		decision, credential_nonce, committed_at_tick
		FROM reviews_and_final WHERE mission_id = ? AND generation = ? AND kind = 'review'
		ORDER BY committed_at_tick ASC`, missionID, generation)
	if err != nil {
		return nil, fmt.Errorf("query reviews: %w", err)
	}
	defer rows.Close()
	var out []domain.Review
	for rows.Next() {
		var r domain.Review
		if err := rows.Scan(
			&r.ReviewID, &r.MissionID, &r.Generation, &r.ReviewerID, &r.ReviewKind, &r.EvidenceHash,
			&r.Decision, &r.CredentialNonce, &r.CommittedAtTick,
		); err != nil {
			return nil, fmt.Errorf("scan review: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertReview appends an independent review row (kind='review').
func (t *sqliteTx) InsertReview(ctx context.Context, r domain.Review) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO reviews_and_final (
		id, kind, mission_id, generation, reviewer_id, review_kind, evidence_hash,
		decision, credential_nonce, committed_at_tick
	) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ReviewID, "review", r.MissionID, r.Generation, r.ReviewerID, string(r.ReviewKind), r.EvidenceHash,
		string(r.Decision), r.CredentialNonce, r.CommittedAtTick,
	)
	if err != nil {
		return fmt.Errorf("insert review: %w", err)
	}
	return nil
}

// GetReviews reads reviews inside the transaction.
func (t *sqliteTx) GetReviews(ctx context.Context, missionID string, generation int64) ([]domain.Review, error) {
	return reviewsQ(ctx, t.tx, missionID, generation)
}

// GetFinal returns the committed final outcome for a generation, if any.
func (s *SQLite) GetFinal(ctx context.Context, missionID string, generation int64) (domain.FinalOutcome, bool, error) {
	var out domain.FinalOutcome
	var nonce string
	err := s.db.QueryRowContext(ctx, `SELECT
		mission_id, generation, decision, credential_nonce
		FROM reviews_and_final WHERE mission_id = ? AND generation = ? AND kind = 'final'`,
		missionID, generation).Scan(&out.MissionID, &out.Generation, &out.TerminalResult, &nonce)
	if err == sql.ErrNoRows {
		return domain.FinalOutcome{}, false, nil
	}
	if err != nil {
		return domain.FinalOutcome{}, false, fmt.Errorf("query final: %w", err)
	}
	out.State = stateForFinal(out.TerminalResult)
	out.CredentialNonce = nonce
	return out, true, nil
}

// stateForFinal maps a terminal result back to its mission state.
func stateForFinal(r domain.FinalResult) domain.State {
	switch r {
	case domain.FinalLaunch:
		return domain.StateLaunched
	case domain.FinalIsolation:
		return domain.StateEngineeringIsolation
	case domain.FinalCancelled:
		return domain.StateCancelled
	default:
		return ""
	}
}

// InsertFinal records the single committed terminal outcome and, for a
// successful launch, the unique credential nonce.
func (t *sqliteTx) InsertFinal(ctx context.Context, missionID string, generation int64, result domain.FinalResult, nonce string, tick int64) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO reviews_and_final (
		id, kind, mission_id, generation, reviewer_id, review_kind, evidence_hash,
		decision, credential_nonce, committed_at_tick
	) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"final-"+missionID, "final", missionID, generation, "", "final", "", string(result), nonce, tick,
	)
	if err != nil {
		return fmt.Errorf("insert final: %w", err)
	}
	return nil
}

// GetFinal reads the committed final outcome inside the transaction.
func (t *sqliteTx) GetFinal(ctx context.Context, missionID string, generation int64) (domain.FinalOutcome, bool, error) {
	var out domain.FinalOutcome
	var nonce string
	err := t.tx.QueryRowContext(ctx, `SELECT
		mission_id, generation, decision, credential_nonce
		FROM reviews_and_final WHERE mission_id = ? AND generation = ? AND kind = 'final'`,
		missionID, generation).Scan(&out.MissionID, &out.Generation, &out.TerminalResult, &nonce)
	if err == sql.ErrNoRows {
		return domain.FinalOutcome{}, false, nil
	}
	if err != nil {
		return domain.FinalOutcome{}, false, fmt.Errorf("query final: %w", err)
	}
	out.State = stateForFinal(out.TerminalResult)
	out.CredentialNonce = nonce
	return out, true, nil
}

// GetIdempotency returns a previously recorded operation-key outcome.
func (s *SQLite) GetIdempotency(ctx context.Context, opKey, operationKind string) (domain.IdempotencyRecord, bool, error) {
	return idempotencyQ(ctx, s.db, opKey, operationKind)
}

func idempotencyQ(ctx context.Context, q queryer, opKey, operationKind string) (domain.IdempotencyRecord, bool, error) {
	var rec domain.IdempotencyRecord
	err := q.QueryRowContext(ctx, `SELECT
		op_key, operation_kind, mission_id, generation, request_hash, response_hash,
		status_code, stable_error_code, created_at_tick
		FROM idempotency_records WHERE op_key = ? AND operation_kind = ?`, opKey, operationKind).Scan(
		&rec.OpKey, &rec.OperationKind, &rec.MissionID, &rec.Generation, &rec.RequestHash,
		&rec.ResponseHash, &rec.StatusCode, &rec.StableErrorCode, &rec.CreatedAtTick,
	)
	if err == sql.ErrNoRows {
		return domain.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return domain.IdempotencyRecord{}, false, fmt.Errorf("query idempotency: %w", err)
	}
	return rec, true, nil
}

// InsertIdempotency records an operation-key outcome.
func (t *sqliteTx) InsertIdempotency(ctx context.Context, rec domain.IdempotencyRecord) error {
	_, err := t.tx.ExecContext(ctx, `INSERT OR IGNORE INTO idempotency_records (
		op_key, operation_kind, mission_id, generation, request_hash, response_hash,
		status_code, stable_error_code, created_at_tick
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		rec.OpKey, rec.OperationKind, rec.MissionID, rec.Generation, rec.RequestHash, rec.ResponseHash,
		rec.StatusCode, rec.StableErrorCode, rec.CreatedAtTick,
	)
	if err != nil {
		return fmt.Errorf("insert idempotency: %w", err)
	}
	return nil
}

// GetIdempotency reads idempotency inside the transaction.
func (t *sqliteTx) GetIdempotency(ctx context.Context, opKey, operationKind string) (domain.IdempotencyRecord, bool, error) {
	return idempotencyQ(ctx, t.tx, opKey, operationKind)
}
