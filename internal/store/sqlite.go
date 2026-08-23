package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLite is a WAL-mode SQLite implementation of Store.
type SQLite struct {
	db *sql.DB
}

// OpenSQLite opens (or creates) the database at path in WAL mode and applies
// the schema. A path of ":memory:" uses a shared in-memory database.
func OpenSQLite(path string) (*SQLite, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file:auvgate?mode=memory&cache=shared"
	} else {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single writer connection serializes concurrent writers while the
	// busy timeout lets racing transactions retry instead of failing with
	// SQLITE_BUSY. Readers may still proceed under WAL.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable busy timeout: %w", err)
	}
	s := &SQLite{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies the documented data model. Ledger tables are created here so
// restart recovery and later sessions rely on a stable schema.
func (s *SQLite) migrate() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS missions (
			mission_id TEXT PRIMARY KEY,
			voyage_id TEXT NOT NULL,
			auv_hull_id TEXT NOT NULL,
			beacon_slot_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			state TEXT NOT NULL,
			locked_payload_hash TEXT NOT NULL,
			route_hash TEXT NOT NULL,
			sea_state_revision INTEGER NOT NULL,
			return_threshold_wh INTEGER NOT NULL,
			depth_limit_m INTEGER NOT NULL,
			trim_min_g INTEGER NOT NULL,
			trim_max_g INTEGER NOT NULL,
			aggregate_version INTEGER NOT NULL,
			terminal_result TEXT NOT NULL DEFAULT '',
			created_at_tick INTEGER NOT NULL,
			updated_at_tick INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS waypoints (
			mission_id TEXT NOT NULL REFERENCES missions(mission_id),
			generation INTEGER NOT NULL,
			seq INTEGER NOT NULL,
			latitude_microdeg INTEGER NOT NULL,
			longitude_microdeg INTEGER NOT NULL,
			target_depth_m INTEGER NOT NULL,
			max_speed_cm_s INTEGER NOT NULL,
			expected_draw_wh INTEGER NOT NULL,
			dwell_seconds INTEGER NOT NULL,
			checksum TEXT NOT NULL,
			PRIMARY KEY (mission_id, generation, seq)
		);`,
		`CREATE TABLE IF NOT EXISTS leases (
			lease_id TEXT PRIMARY KEY,
			resource_type TEXT NOT NULL,
			resource_key TEXT NOT NULL,
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			status TEXT NOT NULL,
			acquired_at_tick INTEGER NOT NULL,
			released_at_tick INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_leases_effective
			ON leases(resource_type, resource_key) WHERE status = 'open';`,
		`CREATE TABLE IF NOT EXISTS authorizations (
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			signer_id TEXT NOT NULL,
			role TEXT NOT NULL,
			payload_hash TEXT NOT NULL,
			op_key TEXT NOT NULL,
			accepted_at_tick INTEGER NOT NULL,
			UNIQUE (mission_id, generation, signer_id),
			UNIQUE (mission_id, generation, role)
		);`,
		`CREATE TABLE IF NOT EXISTS segment_evidence (
			evidence_id TEXT PRIMARY KEY,
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			segment_seq INTEGER NOT NULL,
			attempt_no INTEGER NOT NULL,
			simulator_script_ref TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			response_hash TEXT NOT NULL,
			verdict TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			created_at_tick INTEGER NOT NULL,
			predecessor_id TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS margin_evidence (
			evidence_id TEXT PRIMARY KEY,
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			route_prefix INTEGER NOT NULL,
			energy_used_wh INTEGER NOT NULL,
			return_reserve_wh INTEGER NOT NULL,
			peak_depth_m INTEGER NOT NULL,
			peak_speed_cm_s INTEGER NOT NULL,
			trim_delta_g INTEGER NOT NULL,
			verdict TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			created_at_tick INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS adapter_attempts (
			attempt_id TEXT PRIMARY KEY,
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			adapter_kind TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			status TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			retry_after_tick INTEGER NOT NULL,
			attempt_no INTEGER NOT NULL,
			created_at_tick INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS reviews_and_final (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			reviewer_id TEXT NOT NULL,
			review_kind TEXT NOT NULL,
			evidence_hash TEXT NOT NULL,
			decision TEXT NOT NULL,
			credential_nonce TEXT NOT NULL DEFAULT '',
			committed_at_tick INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS idempotency_records (
			op_key TEXT NOT NULL,
			operation_kind TEXT NOT NULL,
			mission_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			request_hash TEXT NOT NULL,
			response_hash TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			stable_error_code TEXT NOT NULL DEFAULT '',
			created_at_tick INTEGER NOT NULL,
			PRIMARY KEY (op_key, operation_kind)
		);`,
	}
	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// Close closes the underlying database handle.
func (s *SQLite) Close() error { return s.db.Close() }

// sqliteTx wraps a *sql.Tx and implements the Tx surface used by the aggregate.
type sqliteTx struct {
	tx *sql.Tx
}

// WithTx runs fn inside a single SQLite transaction.
func (s *SQLite) WithTx(ctx context.Context, fn func(Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stx := &sqliteTx{tx: tx}
	if err := fn(stx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// queryer is the minimal surface shared by *sql.DB and *sql.Tx so read helpers
// can serve both the connection-level and transaction-level Store methods.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
