package store

import (
	"context"
	"database/sql"
	"fmt"

	"abyssalauvreleasegate/internal/domain"
)

const leaseColumns = `lease_id, resource_type, resource_key, mission_id, generation,
	status, acquired_at_tick, released_at_tick`

func scanLease(rows *sql.Rows) (domain.Lease, error) {
	var l domain.Lease
	err := rows.Scan(
		&l.LeaseID, &l.ResourceType, &l.ResourceKey, &l.MissionID, &l.Generation,
		&l.Status, &l.AcquiredAtTick, &l.ReleasedAtTick,
	)
	return l, err
}

func leasesQ(ctx context.Context, q queryer, query string, args ...any) ([]domain.Lease, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query leases: %w", err)
	}
	defer rows.Close()
	var out []domain.Lease
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lease: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLeases returns every lease row for a mission ordered by resource type.
func (s *SQLite) GetLeases(ctx context.Context, missionID string) ([]domain.Lease, error) {
	return leasesQ(ctx, s.db, `SELECT `+leaseColumns+` FROM leases WHERE mission_id = ? ORDER BY resource_type`, missionID)
}

// InsertLeases inserts one or more lease rows. The effective-open unique index
// enforces at most one open lease per resource; a conflicting insert fails the
// surrounding transaction with a constraint error that the aggregate maps to a
// deterministic lease-conflict envelope.
func (t *sqliteTx) InsertLeases(ctx context.Context, leases []domain.Lease) error {
	for _, l := range leases {
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO leases (
			lease_id, resource_type, resource_key, mission_id, generation,
			status, acquired_at_tick, released_at_tick
		) VALUES (?,?,?,?,?,?,?,?)`,
			l.LeaseID, string(l.ResourceType), l.ResourceKey, l.MissionID, l.Generation,
			string(l.Status), l.AcquiredAtTick, l.ReleasedAtTick,
		); err != nil {
			return fmt.Errorf("insert lease: %w", err)
		}
	}
	return nil
}

// GetLeases reads lease rows for a mission inside the transaction.
func (t *sqliteTx) GetLeases(ctx context.Context, missionID string) ([]domain.Lease, error) {
	return leasesQ(ctx, t.tx, `SELECT `+leaseColumns+` FROM leases WHERE mission_id = ? ORDER BY resource_type`, missionID)
}

// FindOpenLeases returns currently open leases for a resource.
func (t *sqliteTx) FindOpenLeases(ctx context.Context, resourceType, resourceKey string) ([]domain.Lease, error) {
	return leasesQ(ctx, t.tx,
		`SELECT `+leaseColumns+` FROM leases WHERE resource_type = ? AND resource_key = ? AND status = 'open'`,
		resourceType, resourceKey)
}

// ReleaseLeases marks every open lease for a mission as released at tick.
func (t *sqliteTx) ReleaseLeases(ctx context.Context, missionID string, tick int64) error {
	_, err := t.tx.ExecContext(ctx,
		`UPDATE leases SET status = 'released', released_at_tick = ? WHERE mission_id = ? AND status = 'open'`,
		tick, missionID)
	if err != nil {
		return fmt.Errorf("release leases: %w", err)
	}
	return nil
}
