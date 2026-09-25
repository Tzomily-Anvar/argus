package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// draftRoutes registers the close-out draft: the queue of changes typed
// in the panel, kept per sprint so the close can be abandoned and
// resumed after a reload or a restart. The whole queue travels on every
// save, because the panel holds all of it and a merge would only invent
// rows somebody removed.
func (s *Server) draftRoutes(st store.Store) {
	// The draft, or an empty one. No draft is the normal state before a
	// close begins, so it is a 200 with an empty queue rather than a 404
	// the panel would have to special-case.
	s.mux.HandleFunc("GET /api/sprint/draft", func(w http.ResponseWriter, r *http.Request) {
		id, ok := draftSprint(w, r.URL.Query().Get("sprint"))
		if !ok {
			return
		}
		d, err := st.GetDraft(r.Context(), id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeJSON(w, http.StatusOK, map[string]any{"sprint_jira_id": id, "requests": []store.DraftRequest{}})
			return
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, d)
	})

	s.mux.HandleFunc("PUT /api/sprint/draft", func(w http.ResponseWriter, r *http.Request) {
		var d store.Draft
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that draft"})
			return
		}
		if d.SprintJiraID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a draft needs the sprint's Jira id"})
			return
		}
		if err := st.PutDraft(r.Context(), d); err != nil {
			// An unknown sprint is the caller's mistake, as it is for
			// capacity: the panel only opens on a sprint whose report
			// was built, and building it records the sprint.
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})

	// Discard. Deleting what is not there is fine; a second click on
	// Discard is not a mistake worth reporting.
	s.mux.HandleFunc("DELETE /api/sprint/draft", func(w http.ResponseWriter, r *http.Request) {
		id, ok := draftSprint(w, r.URL.Query().Get("sprint"))
		if !ok {
			return
		}
		if err := st.DeleteDraft(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})
}

// draftSprint reads the sprint's Jira id from the query, answering the
// 400 itself when there is not one. It is the Jira id rather than the
// number because the draft is keyed like capacity is, and a number is
// not unique across boards.
func draftSprint(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "which sprint? pass its Jira id as sprint="})
		return 0, false
	}
	return id, true
}
