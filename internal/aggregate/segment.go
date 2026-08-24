package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// SimulateRequest submits or retries simulator evidence for one route segment.
type SimulateRequest struct {
	Generation         int64                `json:"generation"`
	OpKey              string               `json:"op_key"`
	SimulatorScriptRef string               `json:"simulator_script_ref"`
	AdapterStatus      domain.AttemptStatus `json:"adapter_status"`
	Verdict            domain.Verdict       `json:"verdict"`
	ReasonCode         string               `json:"reason_code,omitempty"`
	ResponseHash       string               `json:"response_hash,omitempty"`
}

// SegmentResult reports the effect of one segment simulation attempt.
type SegmentResult struct {
	MissionID    string               `json:"mission_id"`
	Generation   int64                `json:"generation"`
	SegmentSeq   int64                `json:"segment_seq"`
	AttemptNo    int64                `json:"attempt_no"`
	PrefixBefore int64                `json:"prefix_before"`
	PrefixAfter  int64                `json:"prefix_after"`
	Status       domain.AttemptStatus `json:"status"`
	Verdict      domain.Verdict       `json:"verdict"`
	Reasons      []string             `json:"reasons,omitempty"`
	Idempotent   bool                 `json:"idempotent,omitempty"`
}

// simulateBody is the canonical serialization unit for segment idempotency.
type simulateBody struct {
	Generation         int64                `json:"generation"`
	SegmentSeq         int64                `json:"segment_seq"`
	SimulatorScriptRef string               `json:"simulator_script_ref"`
	AdapterStatus      domain.AttemptStatus `json:"adapter_status"`
	Verdict            domain.Verdict       `json:"verdict"`
	ReasonCode         string               `json:"reason_code,omitempty"`
	ResponseHash       string               `json:"response_hash,omitempty"`
}

// SimulateSegment verifies one segment strictly in waypoint order. Accepted
// segments extend the checked prefix by one; unknown, repeated, skipped, stale,
// or simulator-failed segments leave the prefix unchanged and produce auditable
// retry records.
func (a *Aggregate) SimulateSegment(ctx context.Context, missionID string, seq int64, req SimulateRequest) (SegmentResult, error) {
	operation := "simulate_segment"

	body := simulateBody{
		Generation:         req.Generation,
		SegmentSeq:         seq,
		SimulatorScriptRef: req.SimulatorScriptRef,
		AdapterStatus:      req.AdapterStatus,
		Verdict:            req.Verdict,
		ReasonCode:         req.ReasonCode,
		ResponseHash:       req.ResponseHash,
	}
	requestHash, err := domain.HashJSON(body)
	if err != nil {
		return SegmentResult{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var result SegmentResult
	err = a.store.WithTx(ctx, func(tx store.Tx) error {
		m, wps, lerr := loadMissionTx(ctx, tx, missionID, operation)
		if lerr != nil {
			return lerr
		}
		if terr := checkNotTerminal(m, operation); terr != nil {
			return terr
		}
		if terr := checkGeneration(m, req.Generation, operation); terr != nil {
			return terr
		}
		if m.State != domain.StateLeasesHeld && m.State != domain.StateRouteChecking {
			return domain.NewError(domain.ErrCodeStateMachine, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("segments_closed")
		}

		maxSegments := int64(len(wps)) - 1
		if maxSegments < 1 {
			return domain.NewError(domain.ErrCodeSegmentOrder, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("no_segments")
		}

		evidence, gerr := tx.GetSegmentEvidence(ctx, missionID, m.Generation)
		if gerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		prefix := acceptedPrefix(evidence, maxSegments)

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			result = SegmentResult{
				MissionID:    m.MissionID,
				Generation:   m.Generation,
				SegmentSeq:   seq,
				PrefixBefore: prefix,
				PrefixAfter:  prefix,
				Idempotent:   true,
			}
			return nil
		}

		tick := a.now()
		attemptNo := countForSeq(evidence, seq) + 1
		predecessorID := latestEvidenceID(evidence, seq)

		// Ordering and unknown-segment handling: never advance the prefix.
		switch {
		case seq < 1 || seq > maxSegments:
			return a.recordSegmentRetry(ctx, tx, m, &result, seq, prefix, attemptNo, predecessorID,
				domain.VerdictRetry, "unknown_segment", domain.AttemptSuccess, req.SimulatorScriptRef, requestHash, req.ResponseHash, tick)
		case seq <= prefix:
			return a.recordSegmentRetry(ctx, tx, m, &result, seq, prefix, attemptNo, predecessorID,
				domain.VerdictRetry, "sequence_duplicate", req.AdapterStatus, req.SimulatorScriptRef, requestHash, req.ResponseHash, tick)
		case seq > prefix+1:
			return a.recordSegmentRetry(ctx, tx, m, &result, seq, prefix, attemptNo, predecessorID,
				domain.VerdictRetry, "sequence_skipped", req.AdapterStatus, req.SimulatorScriptRef, requestHash, req.ResponseHash, tick)
		}

		// seq == prefix+1: the only case that can advance the prefix.
		if req.AdapterStatus != domain.AttemptSuccess {
			return a.recordSegmentRetry(ctx, tx, m, &result, seq, prefix, attemptNo, predecessorID,
				domain.VerdictRetry, "adapter_"+string(req.AdapterStatus), req.AdapterStatus, req.SimulatorScriptRef, requestHash, req.ResponseHash, tick)
		}
		if req.Verdict != domain.VerdictPass {
			return a.recordSegmentRetry(ctx, tx, m, &result, seq, prefix, attemptNo, predecessorID,
				domain.VerdictFail, req.ReasonCode, domain.AttemptSuccess, req.SimulatorScriptRef, requestHash, req.ResponseHash, tick)
		}

		// Accepted: append pass evidence and advance the prefix by one.
		ev := domain.SegmentEvidence{
			EvidenceID:         a.newID(),
			MissionID:          missionID,
			Generation:         m.Generation,
			SegmentSeq:         seq,
			AttemptNo:          attemptNo,
			SimulatorScriptRef: req.SimulatorScriptRef,
			RequestHash:        requestHash,
			ResponseHash:       req.ResponseHash,
			Verdict:            domain.VerdictPass,
			ReasonCode:         "",
			CreatedAtTick:      tick,
			PredecessorID:      predecessorID,
		}
		if ierr := tx.InsertSegmentEvidence(ctx, ev); ierr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		newPrefix := prefix + 1
		if newPrefix >= maxSegments {
			m.State = domain.StateMarginChecking
		} else if m.State == domain.StateLeasesHeld {
			m.State = domain.StateRouteChecking
		}
		m.UpdatedAtTick = tick
		if uerr := tx.UpdateMission(ctx, m, m.AggregateVersion); uerr != nil {
			if uerr == store.ErrVersionConflict {
				return domain.NewError(domain.ErrCodeFinalAlreadyCommitted, operation).
					WithMission(m.MissionID, m.Generation, m.State)
			}
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 200); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		result = SegmentResult{
			MissionID:    m.MissionID,
			Generation:   m.Generation,
			SegmentSeq:   seq,
			AttemptNo:    attemptNo,
			PrefixBefore: prefix,
			PrefixAfter:  newPrefix,
			Status:       domain.AttemptSuccess,
			Verdict:      domain.VerdictPass,
		}
		return nil
	})
	if err != nil {
		return SegmentResult{}, err
	}
	return result, nil
}

// recordSegmentRetry appends a retry/fail evidence row and returns a result
// whose prefix is unchanged.
func (a *Aggregate) recordSegmentRetry(
	ctx context.Context,
	tx store.Tx,
	m domain.Mission,
	result *SegmentResult,
	seq, prefix, attemptNo int64,
	predecessorID string,
	verdict domain.Verdict,
	reason string,
	status domain.AttemptStatus,
	scriptRef, requestHash, responseHash string,
	tick int64,
) error {
	ev := domain.SegmentEvidence{
		EvidenceID:         a.newID(),
		MissionID:          m.MissionID,
		Generation:         m.Generation,
		SegmentSeq:         seq,
		AttemptNo:          attemptNo,
		SimulatorScriptRef: scriptRef,
		RequestHash:        requestHash,
		ResponseHash:       responseHash,
		Verdict:            verdict,
		ReasonCode:         reason,
		CreatedAtTick:      tick,
		PredecessorID:      predecessorID,
	}
	if err := tx.InsertSegmentEvidence(ctx, ev); err != nil {
		return domain.NewError(domain.ErrCodeInternal, "simulate_segment")
	}
	*result = SegmentResult{
		MissionID:    m.MissionID,
		Generation:   m.Generation,
		SegmentSeq:   seq,
		AttemptNo:    attemptNo,
		PrefixBefore: prefix,
		PrefixAfter:  prefix,
		Status:       status,
		Verdict:      verdict,
		Reasons:      domain.SortedReasons(reason),
	}
	return nil
}

// countForSeq returns the number of existing evidence rows for a segment.
func countForSeq(evidence []domain.SegmentEvidence, seq int64) int64 {
	n := int64(0)
	for _, ev := range evidence {
		if ev.SegmentSeq == seq {
			n++
		}
	}
	return n
}

// latestEvidenceID returns the most recent evidence id for a segment, used as
// the immutable predecessor pointer of the next attempt.
func latestEvidenceID(evidence []domain.SegmentEvidence, seq int64) string {
	var latest domain.SegmentEvidence
	for _, ev := range evidence {
		if ev.SegmentSeq == seq && ev.AttemptNo >= latest.AttemptNo {
			latest = ev
		}
	}
	return latest.EvidenceID
}
