package store

import (
	"context"
	"fmt"

	"abyssalauvreleasegate/internal/domain"
)

// GetSegmentEvidence returns append-only segment evidence ordered by segment
// sequence then attempt number.
func (s *SQLite) GetSegmentEvidence(ctx context.Context, missionID string, generation int64) ([]domain.SegmentEvidence, error) {
	return segmentEvidenceQ(ctx, s.db, missionID, generation)
}

func segmentEvidenceQ(ctx context.Context, q queryer, missionID string, generation int64) ([]domain.SegmentEvidence, error) {
	rows, err := q.QueryContext(ctx, `SELECT
		evidence_id, mission_id, generation, segment_seq, attempt_no,
		simulator_script_ref, request_hash, response_hash, verdict, reason_code,
		created_at_tick, predecessor_id
		FROM segment_evidence WHERE mission_id = ? AND generation = ?
		ORDER BY segment_seq ASC, attempt_no ASC`, missionID, generation)
	if err != nil {
		return nil, fmt.Errorf("query segment evidence: %w", err)
	}
	defer rows.Close()
	var out []domain.SegmentEvidence
	for rows.Next() {
		var ev domain.SegmentEvidence
		if err := rows.Scan(
			&ev.EvidenceID, &ev.MissionID, &ev.Generation, &ev.SegmentSeq, &ev.AttemptNo,
			&ev.SimulatorScriptRef, &ev.RequestHash, &ev.ResponseHash, &ev.Verdict, &ev.ReasonCode,
			&ev.CreatedAtTick, &ev.PredecessorID,
		); err != nil {
			return nil, fmt.Errorf("scan segment evidence: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// InsertSegmentEvidence appends a segment simulation evidence row.
func (t *sqliteTx) InsertSegmentEvidence(ctx context.Context, ev domain.SegmentEvidence) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO segment_evidence (
		evidence_id, mission_id, generation, segment_seq, attempt_no,
		simulator_script_ref, request_hash, response_hash, verdict, reason_code,
		created_at_tick, predecessor_id
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.EvidenceID, ev.MissionID, ev.Generation, ev.SegmentSeq, ev.AttemptNo,
		ev.SimulatorScriptRef, ev.RequestHash, ev.ResponseHash, string(ev.Verdict), ev.ReasonCode,
		ev.CreatedAtTick, ev.PredecessorID,
	)
	if err != nil {
		return fmt.Errorf("insert segment evidence: %w", err)
	}
	return nil
}

// GetSegmentEvidence reads segment evidence inside the transaction.
func (t *sqliteTx) GetSegmentEvidence(ctx context.Context, missionID string, generation int64) ([]domain.SegmentEvidence, error) {
	return segmentEvidenceQ(ctx, t.tx, missionID, generation)
}

// GetMarginEvidence returns append-only margin evidence ordered by tick.
func (s *SQLite) GetMarginEvidence(ctx context.Context, missionID string, generation int64) ([]domain.MarginEvidence, error) {
	return marginEvidenceQ(ctx, s.db, missionID, generation)
}

func marginEvidenceQ(ctx context.Context, q queryer, missionID string, generation int64) ([]domain.MarginEvidence, error) {
	rows, err := q.QueryContext(ctx, `SELECT
		evidence_id, mission_id, generation, route_prefix, energy_used_wh,
		return_reserve_wh, peak_depth_m, peak_speed_cm_s, trim_delta_g, verdict,
		reason_code, created_at_tick
		FROM margin_evidence WHERE mission_id = ? AND generation = ?
		ORDER BY created_at_tick ASC`, missionID, generation)
	if err != nil {
		return nil, fmt.Errorf("query margin evidence: %w", err)
	}
	defer rows.Close()
	var out []domain.MarginEvidence
	for rows.Next() {
		var ev domain.MarginEvidence
		if err := rows.Scan(
			&ev.EvidenceID, &ev.MissionID, &ev.Generation, &ev.RoutePrefix, &ev.EnergyUsedWh,
			&ev.ReturnReserveWh, &ev.PeakDepthM, &ev.PeakSpeedCmS, &ev.TrimDeltaG, &ev.Verdict,
			&ev.ReasonCode, &ev.CreatedAtTick,
		); err != nil {
			return nil, fmt.Errorf("scan margin evidence: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// InsertMarginEvidence appends a margin-check evidence row.
func (t *sqliteTx) InsertMarginEvidence(ctx context.Context, ev domain.MarginEvidence) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO margin_evidence (
		evidence_id, mission_id, generation, route_prefix, energy_used_wh,
		return_reserve_wh, peak_depth_m, peak_speed_cm_s, trim_delta_g, verdict,
		reason_code, created_at_tick
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.EvidenceID, ev.MissionID, ev.Generation, ev.RoutePrefix, ev.EnergyUsedWh,
		ev.ReturnReserveWh, ev.PeakDepthM, ev.PeakSpeedCmS, ev.TrimDeltaG, string(ev.Verdict), ev.ReasonCode,
		ev.CreatedAtTick,
	)
	if err != nil {
		return fmt.Errorf("insert margin evidence: %w", err)
	}
	return nil
}

// GetMarginEvidence reads margin evidence inside the transaction.
func (t *sqliteTx) GetMarginEvidence(ctx context.Context, missionID string, generation int64) ([]domain.MarginEvidence, error) {
	return marginEvidenceQ(ctx, t.tx, missionID, generation)
}
