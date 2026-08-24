// Package httpapi exposes the deterministic JSON API and serves the connected
// browser operations console.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/domain"
)

// Server wires the aggregate service to the HTTP routes and static console.
type Server struct {
	svc       aggregate.Service
	staticDir string
	mux       *http.ServeMux
}

// New builds an HTTP server around the aggregate service.
func New(svc aggregate.Service, staticDir string) *Server {
	s := &Server{svc: svc, staticDir: staticDir, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/missions", s.handleLockMission)
	s.mux.HandleFunc("GET /api/missions/{id}", s.handleGetMission)
	s.mux.HandleFunc("POST /api/missions/{id}/authorizations", s.handleAuthorize)
	s.mux.HandleFunc("POST /api/missions/{id}/leases/acquire", s.handleAcquireLeases)
	s.mux.HandleFunc("POST /api/missions/{id}/segments/{seq}/simulate", s.handleSimulate)
	s.mux.HandleFunc("POST /api/missions/{id}/margins/check", s.handleMargins)
	s.mux.HandleFunc("POST /api/missions/{id}/vessel-confirmations", s.handleVessel)
	s.mux.HandleFunc("POST /api/missions/{id}/reviews", s.handleReview)
	s.mux.HandleFunc("POST /api/missions/{id}/finalize", s.handleFinalize)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	if s.staticDir != "" {
		s.mux.Handle("/", s.staticHandler())
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// staticHandler serves the built browser console, falling back to index.html
// for client-side routing.
func (s *Server) staticHandler() http.Handler {
	fs := http.FileServer(http.Dir(s.staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(s.staticDir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
	})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("httpapi: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, e *domain.Error) {
	writeJSON(w, status, e)
}

// errorStatus maps a stable error code to an HTTP status code.
func errorStatus(code string) int {
	switch code {
	case domain.ErrCodeNotFound:
		return http.StatusNotFound
	case domain.ErrCodeInternal:
		return http.StatusInternalServerError
	case domain.ErrCodeValidation, domain.ErrCodeOverflow, domain.ErrCodePrerequisiteMissing,
		domain.ErrCodeUnqualified, domain.ErrCodeUnknownResult, domain.ErrCodeMarginFailed,
		domain.ErrCodeReviewIncomplete:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusConflict
	}
}

func writeDomainError(w http.ResponseWriter, err error) {
	var de *domain.Error
	if errors.As(err, &de) {
		writeJSON(w, errorStatus(de.Code), de)
		return
	}
	writeJSON(w, http.StatusInternalServerError, domain.NewError(domain.ErrCodeInternal, "unknown"))
}
