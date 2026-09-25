package server

import (
	"errors"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// worklogRoutes registers the read behind the Carryover disclosure: what
// is logged on one ticket right now, so a person can see it before
// proposing to add to it or correct it. Nothing here talks to Jira; the
// entries came in with the issues when the report was built.
func (s *Server) worklogRoutes(svc *sprint.Service) {
	s.mux.HandleFunc("GET /api/sprint/worklog", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "which ticket? pass its key"})
			return
		}
		entries, err := svc.Worklog(sprint.ParseSprintNumber(r.URL.Query().Get("sprint")), key)
		switch {
		case errors.Is(err, sprint.ErrNoReport):
			// The server's state rather than the client's syntax, the
			// same distinction the preview endpoint makes.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, sprint.ErrUnknownKey):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
}
