package httpapi

import (
	"net/http"
	"strconv"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

func (s *Server) handleLockMission(w http.ResponseWriter, r *http.Request) {
	var req aggregate.LockRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "lock_mission"))
		return
	}
	m, err := s.svc.LockMission(r.Context(), req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleGetMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.svc.GetMission(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req aggregate.AuthorizeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "authorize"))
		return
	}
	res, err := s.svc.Authorize(r.Context(), id, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAcquireLeases(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req aggregate.LeaseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "acquire_leases"))
		return
	}
	res, err := s.svc.AcquireLeases(r.Context(), id, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "simulate_segment"))
		return
	}
	var req aggregate.SimulateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "simulate_segment"))
		return
	}
	res, err := s.svc.SimulateSegment(r.Context(), id, seq, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMargins(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req aggregate.MarginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "margins_check"))
		return
	}
	res, err := s.svc.CheckMargins(r.Context(), id, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleVessel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req aggregate.VesselRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "vessel_confirmation"))
		return
	}
	res, err := s.svc.RecordVesselConfirmation(r.Context(), id, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req aggregate.ReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "review"))
		return
	}
	res, err := s.svc.RecordReview(r.Context(), id, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req aggregate.FinalizeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.ErrCodeValidation, "finalize"))
		return
	}
	res, err := s.svc.Finalize(r.Context(), id, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
