package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/backlog"
)

// backlogBatchRoutes registers the bulk bar's three calls: a preview of
// one action over a selection, the apply of a held preview, and the
// reversal that builds a new preview from the audit log. The apply is
// the only one that writes anywhere but the local store, and it refuses
// before writing unless the deployment allows it, the preview is still
// held, the digest is the server's and - for a delete - the person has
// typed the count.
func (s *Server) backlogBatchRoutes(b *backlog.Batch) {
	s.mux.HandleFunc("POST /api/backlog/batch/preview", func(w http.ResponseWriter, r *http.Request) {
		var req backlog.BatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that request"})
			return
		}
		p, err := b.Preview(r.Context(), req)
		switch {
		case errors.Is(err, backlog.ErrUnknownAction), errors.Is(err, backlog.ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, p)
	})

	// A POST rather than a PUT because it is not idempotent from the
	// caller's side: the second one is a 409 by design.
	s.mux.HandleFunc("POST /api/backlog/batch/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Digest  string `json:"digest"`
			Confirm string `json:"confirm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that apply"})
			return
		}
		res, err := b.Apply(r.Context(), r.PathValue("id"), body.Digest, body.Confirm)
		switch {
		case errors.Is(err, backlog.ErrWritesDisabled):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "setting": "ARGUS_SPRINT_ALLOW_WRITES"})
			return
		case errors.Is(err, backlog.ErrDeletesDisabled):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "setting": "ARGUS_BACKLOG_ALLOW_DELETE"})
			return
		case errors.Is(err, backlog.ErrExpired), errors.Is(err, backlog.ErrDigestMismatch), errors.Is(err, backlog.ErrConfirm):
			// The server's state, not the client's syntax: the preview
			// is gone, is not the one shown, or was not confirmed the
			// way a delete has to be. Nothing was written.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// Reversal is a preview like any other: nothing is written here, and
	// the answer is applied through the route above.
	s.mux.HandleFunc("POST /api/backlog/batch/reverse", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch string `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that reversal"})
			return
		}
		p, err := b.Reverse(r.Context(), body.Batch)
		switch {
		case errors.Is(err, backlog.ErrIrreversible), errors.Is(err, backlog.ErrIsReversal):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, backlog.ErrNoWrites):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
}
