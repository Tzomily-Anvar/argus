package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/publish"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// publishRoutes registers publishing the report to Confluence: what the
// panel may offer, the preview, and the publish itself. The publish is
// the second handler in the server that writes anywhere but the local
// store, and like the apply it refuses before writing unless the
// deployment allows writes, the preview is still held, the digest is
// the one the server computed, and the page has not moved.
//
// pub is nil when the sprint tool is wired without a publisher, which a
// test may do; the status then says publishing is not configured.
func (s *Server) publishRoutes(pub *publish.Service) {
	status := func() publish.Status {
		if pub == nil {
			return publish.NotConfigured(publish.ErrNotConfigured.Error(), config.SprintWritesAllowed())
		}
		return pub.Status()
	}

	// Whether publishing is offered at all, whether this deployment may
	// write, and the section switches pre-filled from the setting.
	s.mux.HandleFunc("GET /api/sprint/publish/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, status())
	})

	// The preview: the rendered block against the page's current block,
	// held for fifteen minutes. Nothing is written. Unconfigured is a 200
	// saying so, like the team import, rather than a failure.
	s.mux.HandleFunc("POST /api/sprint/publish/preview", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Sprint   int      `json:"sprint"`
			Sections []string `json:"sections"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that preview request"})
			return
		}
		if pub == nil || !pub.Configured() {
			writeJSON(w, http.StatusOK, status())
			return
		}
		p, err := pub.Preview(r.Context(), body.Sprint, body.Sections)
		switch {
		case errors.Is(err, sprint.ErrNoReport), errors.Is(err, publish.ErrPageGone):
			// The server's state, not the client's syntax: open the
			// report first, or deal with the missing page.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, p)
	})

	// A POST rather than a PUT because it is not idempotent from the
	// caller's side: the second one is a 409 by design.
	s.mux.HandleFunc("POST /api/sprint/publish", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID     string `json:"id"`
			Digest string `json:"digest"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that publish"})
			return
		}
		if pub == nil || !pub.Configured() {
			writeJSON(w, http.StatusConflict, map[string]string{"error": publish.ErrNotConfigured.Error()})
			return
		}
		res, err := pub.Publish(r.Context(), body.ID, body.Digest)
		switch {
		case errors.Is(err, publish.ErrWritesDisabled):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "setting": "ARGUS_SPRINT_ALLOW_WRITES"})
			return
		case errors.Is(err, publish.ErrExpired), errors.Is(err, publish.ErrDigestMismatch), errors.Is(err, publish.ErrMoved):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}
