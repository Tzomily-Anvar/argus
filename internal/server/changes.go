package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// changeRoutes registers the preview half of writing back to Jira. There
// is no apply here and nothing under these routes talks to Jira: a
// preview is computed from the report already in memory, held for
// fifteen minutes, and shown. Registered whether or not writes are
// allowed, because the preview has to be judgeable with writes off.
func (s *Server) changeRoutes(svc *sprint.Service) {
	// Build a preview. This is a POST that writes nothing to Jira, the
	// same distinction the Jira client already makes for its search
	// endpoint: the typed values travel in the body because a dozen
	// estimates and a note do not belong in a query string. Do not
	// "fix" it into a GET.
	s.mux.HandleFunc("POST /api/sprint/changes", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Sprint  int                    `json:"sprint"`
			Changes []sprint.ChangeRequest `json:"changes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read those changes"})
			return
		}
		cs, err := svc.Propose(r.Context(), body.Sprint, body.Changes)
		switch {
		case errors.Is(err, sprint.ErrNoReport):
			// Not the client's syntax but the server's state: the report
			// has to have been opened before anything can be proposed
			// against it.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cs)
	})

	// The held preview, so a reload does not lose it. Gone after fifteen
	// minutes, and the answer says so rather than serving a stale one.
	s.mux.HandleFunc("GET /api/sprint/changes/{id}", func(w http.ResponseWriter, r *http.Request) {
		cs := svc.ChangeSet(r.PathValue("id"))
		if cs == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such change set, or it has expired; build the preview again"})
			return
		}
		writeJSON(w, http.StatusOK, cs)
	})

	// Whether this deployment may write at all. The panel needs it to
	// render a preview without an apply button, rather than an apply
	// button that fails when pressed.
	s.mux.HandleFunc("GET /api/sprint/writes/status", func(w http.ResponseWriter, r *http.Request) {
		allowed := config.SprintWritesAllowed()
		reason := "Writes to Jira are off for this deployment. Previews still work; set ARGUS_SPRINT_ALLOW_WRITES to switch writes on."
		if allowed {
			reason = "Writes to Jira are switched on by ARGUS_SPRINT_ALLOW_WRITES. Every change is previewed first, and only a previewed change set can be applied."
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed": allowed,
			"setting": "ARGUS_SPRINT_ALLOW_WRITES",
			"reason":  reason,
		})
	})
}
