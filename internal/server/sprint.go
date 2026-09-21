package server

import (
	"encoding/json"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"net/http"
	"strconv"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// SprintRoutes registers the sprint report's endpoints. They are only
// registered when the tool is configured, so a deployment without Jira
// credentials simply has no sprint API rather than one that always errors.
func (s *Server) SprintRoutes(svc *sprint.Service, st store.Store) {
	s.sprint = svc
	s.store = st

	// The dropdown. Fetched live because it costs about half a second,
	// and marked with which sprints already have a stored report.
	s.mux.HandleFunc("GET /api/sprint/sprints", func(w http.ResponseWriter, r *http.Request) {
		options, err := svc.Sprints(r.Context(), r.URL.Query().Get("all") == "1")
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sprints": options})
	})

	// One sprint's report. Returns a cached one immediately and refreshes
	// behind it; ?refresh=1 forces a rebuild.
	s.mux.HandleFunc("GET /api/sprint/report", func(w http.ResponseWriter, r *http.Request) {
		number := sprint.ParseSprintNumber(r.URL.Query().Get("sprint"))
		force := r.URL.Query().Get("refresh") == "1"
		res, err := svc.Report(r.Context(), number, force)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// The team's conventions, so the panel can say what a baseline of 10
	// actually means rather than leaving it a bare number.
	s.mux.HandleFunc("GET /api/sprint/conventions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"hours_per_point":    config.HoursPerPoint(),
			"hours_per_day":      config.HoursPerDay(),
			"sprint_length_days": config.JiraSprintLengthDays(),
		})
	})

	// The roster. Baselines have no source in Jira, so they are entered
	// here and stored locally.
	s.mux.HandleFunc("GET /api/sprint/people", func(w http.ResponseWriter, r *http.Request) {
		people, err := st.ListPeople(r.Context(), true)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"people": people})
	})

	s.mux.HandleFunc("PUT /api/sprint/people", func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that person"})
			return
		}
		if err := st.PutPerson(r.Context(), p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// A baseline is an input to every report, so the ones already
		// computed are now wrong.
		svc.Invalidate()
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})

	// Capacity for one sprint. Absence is split by whether it was
	// foreseeable, never by why - see the store package for that reasoning.
	s.mux.HandleFunc("GET /api/sprint/capacity", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.URL.Query().Get("sprint_id"), 10, 64)
		rows, err := st.ListCapacity(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"capacity": rows})
	})

	s.mux.HandleFunc("PUT /api/sprint/capacity", func(w http.ResponseWriter, r *http.Request) {
		var c store.Capacity
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that capacity"})
			return
		}
		// Editing it is the review: an untouched row is the baseline
		// default, and the publish preview warns about those.
		c.Reviewed = true
		if err := st.PutCapacity(r.Context(), c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		svc.Invalidate()
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})
}
