package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// ReviewRequest records independent review evidence for one review kind.
type ReviewRequest struct {
	Generation   int64             `json:"generation"`
	OpKey        string            `json:"op_key"`
	ReviewerID   string            `json:"reviewer_id"`
	ReviewKind   domain.ReviewKind `json:"review_kind"`
	Decision     domain.Verdict    `json:"decision"`
	EvidenceHash string            `json:"evidence_hash"`
}

// ReviewResult reports the effect of one independent review decision.
type ReviewResult struct {
	MissionID      string            `json:"mission_id"`
	Generation     int64             `json:"generation"`
	ReviewID       string            `json:"review_id"`
	ReviewerID     string            `json:"reviewer_id"`
	ReviewKind     domain.ReviewKind `json:"review_kind"`
	Decision       domain.Verdict    `json:"decision"`
	ReviewComplete bool              `json:"review_complete"`
	State          domain.State      `json:"state"`
}

// reviewBody is the canonical serialization unit for review idempotency.
type reviewBody struct {
	Generation   int64             `json:"generation"`
	ReviewerID   string            `json:"reviewer_id"`
	ReviewKind   domain.ReviewKind `json:"review_kind"`
	Decision     domain.Verdict    `json:"decision"`
	EvidenceHash string            `json:"evidence_hash"`
}

// RecordReview records independent review evidence for route, margins, vessel
// confirmation, and sea-state generation. Only qualified reviewers with a
// passing decision contribute toward the two-reviewer completion gate.
func (a *Aggregate) RecordReview(ctx context.Context, missionID string, req ReviewRequest) (ReviewResult, error) {
	operation := "review"

	if !validReviewKind(req.ReviewKind) {
		return ReviewResult{}, domain.NewError(domain.ErrCodeValidation, operation).
			WithReasons("unknown_review_kind")
	}
	if req.Decision != domain.VerdictPass && req.Decision != domain.VerdictFail {
		return ReviewResult{}, domain.NewError(domain.ErrCodeValidation, operation).
			WithReasons("invalid_review_decision")
	}
	if _, ok := a.cat.Reviewer(req.ReviewerID, domain.RoleIndependentReviewer); !ok {
		return ReviewResult{}, domain.NewError(domain.ErrCodeUnqualified, operation).
			WithMission(missionID, req.Generation, "").WithReasons("unqualified_reviewer")
	}

	body := reviewBody{Generation: req.Generation, ReviewerID: req.ReviewerID, ReviewKind: req.ReviewKind, Decision: req.Decision, EvidenceHash: req.EvidenceHash}
	requestHash, err := domain.HashJSON(body)
	if err != nil {
		return ReviewResult{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var result ReviewResult
	err = a.store.WithTx(ctx, func(tx store.Tx) error {
		m, _, lerr := loadMissionTx(ctx, tx, missionID, operation)
		if lerr != nil {
			return lerr
		}
		if terr := checkNotTerminal(m, operation); terr != nil {
			return terr
		}
		if terr := checkGeneration(m, req.Generation, operation); terr != nil {
			return terr
		}
		if m.State != domain.StatePendingVesselConfirmation && m.State != domain.StateLaunchable {
			return domain.NewError(domain.ErrCodeStateMachine, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("reviews_not_open")
		}

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			return a.buildReviewResult(ctx, tx, m, req, &result)
		}

		review := domain.Review{
			ReviewID:        a.newID(),
			MissionID:       missionID,
			Generation:      m.Generation,
			ReviewerID:      req.ReviewerID,
			ReviewKind:      req.ReviewKind,
			EvidenceHash:    req.EvidenceHash,
			Decision:        req.Decision,
			CommittedAtTick: a.now(),
		}
		if ierr := tx.InsertReview(ctx, review); ierr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		// Advance to launchable once vessel confirmation and reviews are both
		// closed for the current generation.
		if m.State == domain.StatePendingVesselConfirmation {
			reviews, gerr := tx.GetReviews(ctx, missionID, m.Generation)
			if gerr != nil {
				return domain.NewError(domain.ErrCodeInternal, operation)
			}
			attempts, gerr := tx.GetAdapterAttempts(ctx, missionID, m.Generation)
			if gerr != nil {
				return domain.NewError(domain.ErrCodeInternal, operation)
			}
			if reviewComplete(reviews) && vesselConfirmed(attempts) {
				m.State = domain.StateLaunchable
				m.UpdatedAtTick = a.now()
				if uerr := tx.UpdateMission(ctx, m, m.AggregateVersion); uerr != nil {
					if uerr == store.ErrVersionConflict {
						return domain.NewError(domain.ErrCodeFinalAlreadyCommitted, operation).
							WithMission(m.MissionID, m.Generation, m.State)
					}
					return domain.NewError(domain.ErrCodeInternal, operation)
				}
			}
		}

		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 200); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		return a.buildReviewResult(ctx, tx, m, req, &result)
	})
	if err != nil {
		return ReviewResult{}, err
	}
	return result, nil
}

func (a *Aggregate) buildReviewResult(ctx context.Context, tx store.Tx, m domain.Mission, req ReviewRequest, result *ReviewResult) error {
	reviews, err := tx.GetReviews(ctx, m.MissionID, m.Generation)
	if err != nil {
		return domain.NewError(domain.ErrCodeInternal, "review")
	}
	result.MissionID = m.MissionID
	result.Generation = m.Generation
	result.ReviewerID = req.ReviewerID
	result.ReviewKind = req.ReviewKind
	result.Decision = req.Decision
	result.ReviewComplete = reviewComplete(reviews)
	result.State = m.State
	return nil
}

// validReviewKind reports whether the review kind is one of the four documented
// independent review kinds.
func validReviewKind(k domain.ReviewKind) bool {
	switch k {
	case domain.ReviewRoute, domain.ReviewMargins, domain.ReviewVesselConfirmation, domain.ReviewSeaState:
		return true
	default:
		return false
	}
}
