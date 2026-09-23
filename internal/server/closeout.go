package server

import (
	"errors"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// closeoutRoutes registers the "Close out sprint" panel's reads: the
// checklist, and the draft that keeps a half-finished close between
// sittings. Nothing here talks to Jira; the checklist is computed from
// the report already in memory, and the draft lives in the store.
func (s *Server) closeoutRoutes(svc *sprint.Service, st store.Store) {
	// The checklist for one sprint, each step pre-filled with the report's
	// best suggestion and labelled as such by the panel.
	s.mux.HandleFunc("GET /api/sprint/closeout", func(w http.ResponseWriter, r *http.Request) {
		model, err := svc.Closeout(r.Context(), sprint.ParseSprintNumber(r.URL.Query().Get("sprint")))
		switch {
		case errors.Is(err, sprint.ErrNoReport):
			// The server's state rather than the client's syntax, the
			// same distinction the preview endpoint makes.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, model)
	})

	s.draftRoutes(st)
}
