package aggregate

import (
	"context"

	"abyssalauvreleasegate/internal/domain"
	"abyssalauvreleasegate/internal/store"
)

// AuthorizeRequest records one signer for the two-person technical
// authorization gate.
type AuthorizeRequest struct {
	Generation  int64             `json:"generation"`
	OpKey       string            `json:"op_key"`
	SignerID    string            `json:"signer_id"`
	Role        domain.SignerRole `json:"role"`
	PayloadHash string            `json:"payload_hash"`
}

// AuthorizationResult reports the effect of a technical authorization.
type AuthorizationResult struct {
	MissionID      string              `json:"mission_id"`
	Generation     int64               `json:"generation"`
	SignerID       string              `json:"signer_id"`
	Role           domain.SignerRole   `json:"role"`
	Authorized     bool                `json:"authorized"`
	RemainingRoles []domain.SignerRole `json:"remaining_roles,omitempty"`
}

// authorizeBody is the canonical serialization unit for authorization idempotency.
type authorizeBody struct {
	Generation  int64             `json:"generation"`
	SignerID    string            `json:"signer_id"`
	Role        domain.SignerRole `json:"role"`
	PayloadHash string            `json:"payload_hash"`
}

// Authorize records a technical authorization with op-key idempotency and
// stable errors for duplicate signer, role overlap, or payload conflict.
func (a *Aggregate) Authorize(ctx context.Context, missionID string, req AuthorizeRequest) (AuthorizationResult, error) {
	operation := "authorize"

	if req.Role != domain.RoleTechnicalAuthorizer && req.Role != domain.RoleSafetyAuthorizer {
		return AuthorizationResult{}, domain.NewError(domain.ErrCodeValidation, operation).
			WithReasons("unknown_authorization_role")
	}
	if _, ok := a.cat.Reviewer(req.SignerID, req.Role); !ok {
		return AuthorizationResult{}, domain.NewError(domain.ErrCodeUnqualified, operation).
			WithMission(missionID, req.Generation, "").WithReasons("unqualified_signer")
	}

	body := authorizeBody{Generation: req.Generation, SignerID: req.SignerID, Role: req.Role, PayloadHash: req.PayloadHash}
	requestHash, err := domain.HashJSON(body)
	if err != nil {
		return AuthorizationResult{}, domain.NewError(domain.ErrCodeInternal, operation)
	}

	var result AuthorizationResult
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
		if m.State != domain.StatePendingAuthorization {
			return domain.NewError(domain.ErrCodeStateMachine, operation).
				WithMission(m.MissionID, m.Generation, m.State).WithReasons("authorization_closed")
		}
		if req.PayloadHash != m.LockedPayloadHash {
			return domain.NewError(domain.ErrCodePayloadConflict, operation).
				WithMission(m.MissionID, m.Generation, m.State)
		}

		if replay, ierr := a.resolveIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, operation); ierr != nil {
			return ierr
		} else if replay {
			return a.replayAuthorize(ctx, tx, m, req, &result)
		}

		existing, gerr := tx.GetAuthorizations(ctx, missionID, m.Generation)
		if gerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		for _, e := range existing {
			if e.SignerID == req.SignerID {
				if e.Role == req.Role {
					return domain.NewError(domain.ErrCodeDuplicateSigner, operation).
						WithMission(m.MissionID, m.Generation, m.State)
				}
				return domain.NewError(domain.ErrCodeRoleOverlap, operation).
					WithMission(m.MissionID, m.Generation, m.State)
			}
			if e.Role == req.Role {
				return domain.NewError(domain.ErrCodeRoleOverlap, operation).
					WithMission(m.MissionID, m.Generation, m.State)
			}
		}

		auth := domain.Authorization{
			MissionID:      missionID,
			Generation:     m.Generation,
			SignerID:       req.SignerID,
			Role:           req.Role,
			PayloadHash:    req.PayloadHash,
			OpKey:          req.OpKey,
			AcceptedAtTick: a.now(),
		}
		if ierr := tx.InsertAuthorization(ctx, auth); ierr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}
		if rerr := recordIdempotency(ctx, tx, req.OpKey, operation, missionID, m.Generation, requestHash, "", 201); rerr != nil {
			return domain.NewError(domain.ErrCodeInternal, operation)
		}

		return a.buildAuthorizeResult(ctx, tx, m, req, &result)
	})
	if err != nil {
		return AuthorizationResult{}, err
	}
	return result, nil
}

func (a *Aggregate) replayAuthorize(ctx context.Context, tx store.Tx, m domain.Mission, req AuthorizeRequest, result *AuthorizationResult) error {
	return a.buildAuthorizeResult(ctx, tx, m, req, result)
}

func (a *Aggregate) buildAuthorizeResult(ctx context.Context, tx store.Tx, m domain.Mission, req AuthorizeRequest, result *AuthorizationResult) error {
	existing, err := tx.GetAuthorizations(ctx, m.MissionID, m.Generation)
	if err != nil {
		return domain.NewError(domain.ErrCodeInternal, "authorize")
	}
	filled := make(map[domain.SignerRole]bool)
	for _, e := range existing {
		filled[e.Role] = true
	}
	var remaining []domain.SignerRole
	for _, role := range domain.TechnicalAuthorizationRoles() {
		if !filled[role] {
			remaining = append(remaining, role)
		}
	}
	result.MissionID = m.MissionID
	result.Generation = m.Generation
	result.SignerID = req.SignerID
	result.Role = req.Role
	result.Authorized = len(remaining) == 0
	result.RemainingRoles = remaining
	return nil
}
