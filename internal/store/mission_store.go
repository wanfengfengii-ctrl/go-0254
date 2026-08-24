package store

import (
	"context"
	"database/sql"
	"fmt"

	"abyssalauvreleasegate/internal/domain"
)

const missionColumns = `mission_id, voyage_id, auv_hull_id, beacon_slot_id, generation, state,
	locked_payload_hash, route_hash, sea_state_revision,
	return_threshold_wh, depth_limit_m, trim_min_g, trim_max_g,
	aggregate_version, terminal_result, created_at_tick, updated_at_tick`

func scanMission(row *sql.Row) (domain.Mission, error) {
	var m domain.Mission
	err := row.Scan(
		&m.MissionID, &m.VoyageID, &m.AUVHullID, &m.BeaconSlotID, &m.Generation, &m.State,
		&m.LockedPayloadHash, &m.RouteHash, &m.SeaStateRevision,
		&m.ReturnThresholdWh, &m.DepthLimitM, &m.TrimMinG, &m.TrimMaxG,
		&m.AggregateVersion, &m.TerminalResult, &m.CreatedAtTick, &m.UpdatedAtTick,
	)
	return m, err
}

func getMissionQ(ctx context.Context, q queryer, missionID string) (domain.Mission, error) {
	m, err := scanMission(q.QueryRowContext(ctx,
		`SELECT `+missionColumns+` FROM missions WHERE mission_id = ?`, missionID))
	if err == sql.ErrNoRows {
		return domain.Mission{}, ErrNotFound
	}
	if err != nil {
		return domain.Mission{}, fmt.Errorf("scan mission: %w", err)
	}
	return m, nil
}

func getWaypointsQ(ctx context.Context, q queryer, missionID string) ([]domain.Waypoint, error) {
	rows, err := q.QueryContext(ctx, `SELECT
		mission_id, generation, seq, latitude_microdeg, longitude_microdeg,
		target_depth_m, max_speed_cm_s, expected_draw_wh, dwell_seconds, checksum
		FROM waypoints WHERE mission_id = ? ORDER BY seq ASC`, missionID)
	if err != nil {
		return nil, fmt.Errorf("query waypoints: %w", err)
	}
	defer rows.Close()

	var waypoints []domain.Waypoint
	for rows.Next() {
		var w domain.Waypoint
		if err := rows.Scan(
			&w.MissionID, &w.Generation, &w.Seq, &w.LatitudeMicrodeg, &w.LongitudeMicrodeg,
			&w.TargetDepthM, &w.MaxSpeedCmS, &w.ExpectedDrawWh, &w.DwellSeconds, &w.Checksum,
		); err != nil {
			return nil, fmt.Errorf("scan waypoint: %w", err)
		}
		waypoints = append(waypoints, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate waypoints: %w", err)
	}
	return waypoints, nil
}

// CreateMission persists a new locked mission generation and its waypoints.
func (s *SQLite) CreateMission(ctx context.Context, m domain.Mission, waypoints []domain.Waypoint) error {
	return s.WithTx(ctx, func(tx Tx) error {
		return insertMissionTx(ctx, tx.(*sqliteTx).tx, m, waypoints)
	})
}

func insertMissionTx(ctx context.Context, q queryer, m domain.Mission, waypoints []domain.Waypoint) error {
	_, err := q.ExecContext(ctx, `INSERT INTO missions (
		mission_id, voyage_id, auv_hull_id, beacon_slot_id, generation, state,
		locked_payload_hash, route_hash, sea_state_revision,
		return_threshold_wh, depth_limit_m, trim_min_g, trim_max_g,
		aggregate_version, terminal_result, created_at_tick, updated_at_tick
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.MissionID, m.VoyageID, m.AUVHullID, m.BeaconSlotID, m.Generation, string(m.State),
		m.LockedPayloadHash, m.RouteHash, m.SeaStateRevision,
		m.ReturnThresholdWh, m.DepthLimitM, m.TrimMinG, m.TrimMaxG,
		m.AggregateVersion, m.TerminalResult, m.CreatedAtTick, m.UpdatedAtTick,
	)
	if err != nil {
		return fmt.Errorf("insert mission: %w", err)
	}
	for _, w := range waypoints {
		if _, err := q.ExecContext(ctx, `INSERT INTO waypoints (
			mission_id, generation, seq, latitude_microdeg, longitude_microdeg,
			target_depth_m, max_speed_cm_s, expected_draw_wh, dwell_seconds, checksum
		) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			w.MissionID, w.Generation, w.Seq, w.LatitudeMicrodeg, w.LongitudeMicrodeg,
			w.TargetDepthM, w.MaxSpeedCmS, w.ExpectedDrawWh, w.DwellSeconds, w.Checksum,
		); err != nil {
			return fmt.Errorf("insert waypoint %d: %w", w.Seq, err)
		}
	}
	return nil
}

// GetMission returns a mission aggregate and its ordered waypoints.
func (s *SQLite) GetMission(ctx context.Context, missionID string) (domain.Mission, []domain.Waypoint, error) {
	m, err := getMissionQ(ctx, s.db, missionID)
	if err != nil {
		return domain.Mission{}, nil, err
	}
	waypoints, err := getWaypointsQ(ctx, s.db, missionID)
	if err != nil {
		return domain.Mission{}, nil, err
	}
	return m, waypoints, nil
}

// UpdateMission applies a compare-and-set update guarded by aggregate_version.
func (t *sqliteTx) UpdateMission(ctx context.Context, m domain.Mission, expectedVersion int64) error {
	res, err := t.tx.ExecContext(ctx, `UPDATE missions SET
		state = ?, terminal_result = ?, aggregate_version = aggregate_version + 1,
		updated_at_tick = ?
		WHERE mission_id = ? AND aggregate_version = ?`,
		string(m.State), m.TerminalResult, m.UpdatedAtTick, m.MissionID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update mission: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}

// GetMission returns a mission and ordered waypoints inside the transaction.
func (t *sqliteTx) GetMission(ctx context.Context, missionID string) (domain.Mission, []domain.Waypoint, error) {
	m, err := getMissionQ(ctx, t.tx, missionID)
	if err != nil {
		return domain.Mission{}, nil, err
	}
	waypoints, err := getWaypointsQ(ctx, t.tx, missionID)
	if err != nil {
		return domain.Mission{}, nil, err
	}
	return m, waypoints, nil
}
