package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// applyRoutes registers the half of writing back to Jira that writes: the
// apply of a held preview, the reversal that builds a new preview from
// the audit log, and the log itself. The apply is the only handler in the
// server that causes a write anywhere but the local store, and it refuses
// before writing unless the deployment allows writes, the preview is
// still held, and the digest is the one the server computed.
func (s *Server) applyRoutes(svc *sprint.Service, st store.Store) {
	// A POST rather than a PUT because it is not idempotent from the
	// caller's side: the second one is a 409 by design, which is what
	// stops a double-click writing twice.
	s.mux.HandleFunc("POST /api/sprint/changes/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Digest string `json:"digest"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that apply"})
			return
		}
		res, err := svc.Apply(r.Context(), r.PathValue("id"), body.Digest)
		switch {
		case errors.Is(err, sprint.ErrWritesDisabled):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "setting": "ARGUS_SPRINT_ALLOW_WRITES"})
			return
		case errors.Is(err, sprint.ErrExpired), errors.Is(err, sprint.ErrDigestMismatch):
			// The server's state, not the client's syntax: the preview
			// is gone, or is not the one that was shown. Nothing was
			// written, and the answer is to build it again.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// Reversal is a preview like any other. Nothing is written here; the
	// answer is a held change set with a digest, to be looked at and
	// applied through the route above.
	s.mux.HandleFunc("POST /api/sprint/changes/reverse", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChangeSet string `json:"change_set"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that reversal"})
			return
		}
		cs, err := svc.Reverse(r.Context(), body.ChangeSet)
		switch {
		case errors.Is(err, sprint.ErrNoWrites):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cs)
	})

	// The audit log, newest first. Local data about named colleagues,
	// served on loopback to the operator who made the writes.
	s.mux.HandleFunc("GET /api/sprint/writes", func(w http.ResponseWriter, r *http.Request) {
		limit := sprint.ParseSprintNumber(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 100
		}
		writes, err := st.ListWrites(r.Context(), limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if writes == nil {
			// An array where an array was promised, not null.
			writes = []store.WriteRecord{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"writes": writes})
	})
}
