package server

import (
	"encoding/json"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/backlog"
	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// BacklogRoutes registers the backlog tool's endpoints. Registered only
// when the tool is configured, like the sprint report's, so a
// deployment without it has no backlog API rather than one that errors.
//
// Every request here counts as somebody looking at the backlog, which
// is what keeps its sweep at the configured pace; a visit to the pull
// request dashboard says nothing about whether the backlog is being
// groomed, so it does not.
func (s *Server) BacklogRoutes(svc *backlog.Service, st store.Store, b *backlog.Batch) {
	if b != nil {
		s.backlogBatchRoutes(b)
	}
	s.backlog = svc
	handle := func(pattern string, fn http.HandlerFunc) {
		s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			svc.Touch()
			fn(w, r)
		})
	}

	// The whole page in one answer, drawn over the last sweep and the
	// acknowledgements and roster as they stand right now.
	handle("GET /api/backlog", func(w http.ResponseWriter, r *http.Request) {
		v, err := svc.View(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, v)
	})

	// Asks for a sweep now. Returns at once; the browser polls the view
	// and sees building go false.
	handle("POST /api/backlog/refresh", func(w http.ResponseWriter, r *http.Request) {
		svc.Refresh()
		writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
	})

	// Acknowledge: record each ticket's current updated timestamp, as
	// the last sweep read it, so the ticket hides until it changes. A
	// key the sweep does not hold cannot be watermarked and is named in
	// the answer rather than silently dropped.
	handle("POST /api/backlog/ack", func(w http.ResponseWriter, r *http.Request) {
		keys, ok := readKeys(w, r, "keys")
		if !ok {
			return
		}
		marks := svc.Watermarks(keys)
		acks := make([]store.Ack, 0, len(marks))
		unknown := []string{}
		for _, k := range keys {
			if wm, found := marks[k]; found {
				acks = append(acks, store.Ack{Kind: store.AckTicket, Key: k, Watermark: wm})
			} else {
				unknown = append(unknown, k)
			}
		}
		if err := st.PutAcks(r.Context(), acks); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"acknowledged": len(acks), "unknown": unknown})
	})

	handle("DELETE /api/backlog/ack", func(w http.ResponseWriter, r *http.Request) {
		keys, ok := readKeys(w, r, "keys")
		if !ok {
			return
		}
		if err := st.DeleteAcks(r.Context(), store.AckTicket, keys); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"withdrawn": len(keys)})
	})

	// The inbox, with dismissals applied. Dismissing is the same
	// watermark mechanism under another name, keyed by item id.
	handle("GET /api/backlog/inbox", func(w http.ResponseWriter, r *http.Request) {
		items, warnings, err := svc.InboxView(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "warnings": warnings})
	})

	handle("POST /api/backlog/inbox/dismiss", func(w http.ResponseWriter, r *http.Request) {
		ids, ok := readKeys(w, r, "ids")
		if !ok {
			return
		}
		marks := svc.InboxWatermarks(ids)
		acks := make([]store.Ack, 0, len(marks))
		unknown := []string{}
		for _, id := range ids {
			if wm, found := marks[id]; found {
				acks = append(acks, store.Ack{Kind: store.AckInbox, Key: id, Watermark: wm})
			} else {
				unknown = append(unknown, id)
			}
		}
		if err := st.PutAcks(r.Context(), acks); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"dismissed": len(acks), "unknown": unknown})
	})

	handle("DELETE /api/backlog/inbox/dismiss", func(w http.ResponseWriter, r *http.Request) {
		ids, ok := readKeys(w, r, "ids")
		if !ok {
			return
		}
		if err := st.DeleteAcks(r.Context(), store.AckInbox, ids); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"withdrawn": len(ids)})
	})

	// The operations roster, with the reporters seen in the backlog as
	// candidates, most frequent first, so a person is ticked rather than
	// their account id copied about.
	// Who the operations team's Atlassian team says is on it, merged with
	// the roster already stored. Optional configuration, so an install
	// without the team id answers 200 with the reason rather than an
	// error; and this only ever proposes - the browser saves what was
	// ticked through PUT /api/backlog/ops-roster.
	handle("GET /api/backlog/ops-team", func(w http.ResponseWriter, r *http.Request) {
		if config.AtlassianOrgID() == "" || config.BacklogOpsTeamID() == "" {
			writeJSON(w, http.StatusOK, sprint.NotConfigured(
				"Set ARGUS_ATLASSIAN_ORG_ID and ARGUS_BACKLOG_OPS_TEAM_ID to import the operations roster "+
					"from an Atlassian team. Until then it is kept here by hand."))
			return
		}
		email, token, err := config.JiraCredentials()
		if err != nil {
			writeJSON(w, http.StatusOK, sprint.NotConfigured(err.Error()))
			return
		}
		team, err := jira.NewTeams(config.AtlassianOrgID(), config.BacklogOpsTeamID(), email, token, config.HTTPTimeout())
		if err != nil {
			writeJSON(w, http.StatusOK, sprint.NotConfigured(err.Error()))
			return
		}
		members, err := st.ListOps(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		roster := make([]store.Person, 0, len(members))
		for _, m := range members {
			roster = append(roster, store.Person{AccountID: m.AccountID, Name: m.Name, Active: true})
		}
		imported, err := sprint.TeamImport(r.Context(), team, svc, roster)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, imported)
	})

	handle("GET /api/backlog/ops-roster", func(w http.ResponseWriter, r *http.Request) {
		members, err := st.ListOps(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"members": members, "candidates": svc.Reporters()})
	})

	handle("PUT /api/backlog/ops-roster", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Members []store.OpsMember `json:"members"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that roster"})
			return
		}
		for _, m := range body.Members {
			// The account id is the join key to who reported what; a
			// member without one matches nobody and is refused here rather
			// than stored and puzzled over later.
			if m.AccountID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a roster member needs an account id"})
				return
			}
		}
		if err := st.PutOps(r.Context(), body.Members); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	})
}

// readKeys decodes a body of the form {"<field>": ["...", ...]} and
// refuses an empty or unreadable one, since acknowledging nothing is a
// mistake in the caller rather than a request to honour.
func readKeys(w http.ResponseWriter, r *http.Request, field string) ([]string, bool) {
	var body map[string][]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that request"})
		return nil, false
	}
	keys := body[field]
	if len(keys) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing named in " + field})
		return nil, false
	}
	return keys, true
}
