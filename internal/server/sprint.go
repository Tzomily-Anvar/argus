package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
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

	// The estimate-against-actual run across sprints.
	//
	// Its own endpoint rather than part of the report, because it costs a
	// Jira sweep per sprint it does not already hold and the report must
	// not get slower for a section further down the page. It answers with
	// whatever has been swept and a flag saying more is coming, which the
	// browser polls on exactly as it polls a rebuilding report.
	s.mux.HandleFunc("GET /api/sprint/calibration", func(w http.ResponseWriter, r *http.Request) {
		number := sprint.ParseSprintNumber(r.URL.Query().Get("sprint"))
		span := sprint.ParseSprintNumber(r.URL.Query().Get("span"))
		trend, err := svc.CalibrationTrend(r.Context(), number, span)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, trend)
	})

	// The team's conventions, so the panel can say what a baseline of 10
	// actually means rather than leaving it a bare number.
	s.mux.HandleFunc("GET /api/sprint/conventions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"hours_per_point":     config.HoursPerPoint(),
			"hours_per_day":       config.HoursPerDay(),
			"sprint_length_days":  config.JiraSprintLengthDays(),
			"worklog_attribution": config.JiraWorklogAttribution(),
			"absence_cost":        config.SprintAbsenceCost(),
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
		// The account id is the join key to everything Jira says about
		// this person. A row without one is unreachable rather than
		// harmless, so it is refused here rather than stored and puzzled
		// over later.
		if p.AccountID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a person needs an account id"})
			return
		}
		if err := st.PutPerson(r.Context(), p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// A baseline is an input to every report, so the ones already
		// computed are now wrong. Recompute rather than discard: the
		// browser refetches immediately after this returns, and a
		// discarded report would make it wait for a full Jira sweep and
		// show the old number until that landed.
		svc.Recompute(r.Context())
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})

	// Who the team's Atlassian team says is on it, merged with the roster
	// already stored.
	//
	// Optional configuration, so an unconfigured install answers 200 with
	// the reason rather than an error. The panel then explains what to set
	// in place of the import button, which is a good deal better than a
	// button that fails when pressed.
	//
	// This only ever proposes. Nothing is written here: the browser saves
	// the people that were ticked through PUT /api/sprint/people, which is
	// the same write, with the same explicit Save, as typing them in.
	s.mux.HandleFunc("GET /api/sprint/team", func(w http.ResponseWriter, r *http.Request) {
		if !config.AtlassianTeamConfigured() {
			writeJSON(w, http.StatusOK, sprint.NotConfigured(
				"Set ARGUS_ATLASSIAN_ORG_ID and ARGUS_ATLASSIAN_TEAM_ID to import the roster "+
					"from your Atlassian team. Until then the roster is maintained here by hand."))
			return
		}
		email, token, err := config.JiraCredentials()
		if err != nil {
			writeJSON(w, http.StatusOK, sprint.NotConfigured(err.Error()))
			return
		}
		team, err := jira.NewTeams(
			config.AtlassianOrgID(), config.AtlassianTeamID(),
			email, token, config.HTTPTimeout())
		if err != nil {
			writeJSON(w, http.StatusOK, sprint.NotConfigured(err.Error()))
			return
		}

		roster, err := st.ListPeople(r.Context(), true)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		imported, err := sprint.TeamImport(r.Context(), team, svc, roster)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, imported)
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
		svc.Recompute(r.Context())
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})

	// Whether a human has been through a sprint's availability.
	//
	// This is a sprint-level fact because "everybody was available" is one:
	// it produces no capacity rows, and without somewhere to record it a
	// sprint that was checked and needed nothing looks exactly like a
	// sprint nobody has opened. Passing reviewed false reopens it.
	s.mux.HandleFunc("PUT /api/sprint/review", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SprintJiraID int64 `json:"sprint_jira_id"`
			Reviewed     bool  `json:"reviewed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that review"})
			return
		}
		if err := st.SetCapacityReviewed(r.Context(), body.SprintJiraID, body.Reviewed); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		svc.Recompute(r.Context())
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})
}
