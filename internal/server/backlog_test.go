package server_test

// The backlog endpoints against a stand-in Jira that serves one small
// backlog and fails the test if anything tries to write to it. That is
// the property worth holding on this side of the tool: every request
// the sweep and the handlers make is a read, and acknowledging or
// dismissing touches the store alone.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/backlog"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

const backlogPointsField = "customfield_10016"

// fakeBacklogJira answers the reads one backlog sweep makes: the field
// catalogue, the board and its sprints, the project's issue types, the
// searches, and the Confluence content search. The search endpoint is
// told apart by its JQL.
func fakeBacklogJira(t *testing.T) *httptest.Server {
	t.Helper()
	backlogIssues := []map[string]any{
		{"id": "10001", "key": "ABC-1", "fields": map[string]any{
			"summary": "Fresh and bare", "issuetype": map[string]any{"name": "Task"},
			"status":  map[string]any{"name": "To Refine", "statusCategory": map[string]any{"key": "new"}},
			"created": "2026-09-27T09:00:00.000+0000", "updated": "2026-09-27T09:00:00.000+0000",
			"reporter": map[string]any{"accountId": "acc-r1", "displayName": "Reporter One"},
			"labels":   []string{},
		}},
		{"id": "10002", "key": "ABC-2", "fields": map[string]any{
			"summary": "A story", "issuetype": map[string]any{"name": "Story"},
			"status":  map[string]any{"name": "In Progress", "statusCategory": map[string]any{"key": "indeterminate"}},
			"created": "2026-01-06T09:00:00.000+0000", "updated": "2026-01-18T09:00:00.000+0000",
			"reporter": map[string]any{"accountId": "acc-r2", "displayName": "Reporter Two"},
			"labels":   []string{"Ops"},
			"parent": map[string]any{"key": "ABC-100", "fields": map[string]any{
				"summary": "[Build] Platform", "issuetype": map[string]any{"name": "Epic"}}},
			backlogPointsField: 5,
		}},
	}
	epics := []map[string]any{{"id": "10100", "key": "ABC-100", "fields": map[string]any{"summary": "[Build] Platform"}}}
	mentioned := []map[string]any{{"id": "10009", "key": "XYZ-9", "fields": map[string]any{
		"summary": "Named you", "updated": "2026-09-27T12:00:00.000+0000"}}}
	answer := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/3/field":
			answer(w, []map[string]any{
				{"id": backlogPointsField, "name": "Story Points", "custom": true},
				{"id": "customfield_10017", "name": "Story point estimate", "custom": true},
				{"id": "customfield_10020", "name": "Sprint", "custom": true},
			})
		case "GET /rest/agile/1.0/board":
			if r.URL.Query().Get("projectKeyOrId") != "ABC" {
				t.Errorf("board lookup for the wrong project: %s", r.URL)
			}
			answer(w, map[string]any{"values": []map[string]any{{"id": 1, "name": "ABC board"}}})
		case "GET /rest/agile/1.0/board/1/configuration":
			answer(w, map[string]any{"id": 1, "estimation": map[string]any{
				"type": "field", "field": map[string]any{"fieldId": backlogPointsField, "displayName": "Story Points"}}})
		case "GET /rest/agile/1.0/board/1/sprint":
			answer(w, map[string]any{"values": []map[string]any{
				{"id": 2, "name": "Sprint 2", "state": "active"}, {"id": 3, "name": "Sprint 3", "state": "future"}}})
		case "GET /rest/api/3/project/ABC":
			answer(w, map[string]any{"issueTypes": []map[string]any{
				{"name": "Epic", "hierarchyLevel": 1}, {"name": "Story", "hierarchyLevel": 0},
				{"name": "Task", "hierarchyLevel": 0}, {"name": "Sub-task", "hierarchyLevel": -1}}})
		case "POST /rest/api/3/search/jql":
			var body struct {
				JQL string `json:"jql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch {
			case strings.Contains(body.JQL, "issuetype = Epic"):
				answer(w, map[string]any{"issues": epics, "isLast": true})
			case strings.HasPrefix(body.JQL, "text ~ currentUser()"):
				answer(w, map[string]any{"issues": mentioned, "isLast": true})
			case strings.Contains(body.JQL, "currentUser()"):
				answer(w, map[string]any{"issues": []any{}, "isLast": true})
			default:
				answer(w, map[string]any{"issues": backlogIssues, "isLast": true})
			}
		case "GET /wiki/rest/api/content/search":
			if strings.HasPrefix(r.URL.Query().Get("cql"), "watcher") {
				answer(w, map[string]any{"results": []any{}})
				return
			}
			answer(w, map[string]any{"results": []map[string]any{{
				"id": "4242", "type": "page", "title": "Runbook", "space": map[string]any{"name": "Docs"},
				"version": map[string]any{"when": "2026-09-27T08:00:00.000Z"},
				"_links":  map[string]any{"webui": "/spaces/DOCS/pages/4242"}}}})
		default:
			t.Errorf("the stand-in Jira got %s %s, which the backlog's read side must never make", r.Method, r.URL)
			http.Error(w, "not here", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// backlogServer builds a server with the tool wired and one sweep done.
func backlogServer(t *testing.T, sweep bool) *server.Server {
	t.Helper()
	fake := fakeBacklogJira(t)
	client, err := jira.New(fake.URL, "operator@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := backlog.NewService(client, st, backlog.Config{
		Project: "ABC", BaseURL: fake.URL,
		StaleDays: 14, NewDays: 3, OperationsLabel: "Operations", LegacyLabels: []string{"Ops"},
		InboxDays: 3, StoryLinkTypes: []string{"Blocks"}, ContainerTypes: []string{"Story"},
	}, 0)
	if sweep {
		if err := svc.Sweep(t.Context()); err != nil {
			t.Fatalf("sweeping the stand-in: %v", err)
		}
	}
	srv := server.New(nil)
	srv.BacklogRoutes(svc, st, nil)
	return srv
}

func decodeView(t *testing.T, rec *httptest.ResponseRecorder) backlog.View {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var v backlog.View
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding the view: %v", err)
	}
	return v
}

func groupKeys(v backlog.View, id string) []string {
	for _, g := range v.Groups {
		if g.ID == id {
			return g.Keys
		}
	}
	return nil
}

func TestBacklogViewAfterASweep(t *testing.T) {
	t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "0")
	srv := backlogServer(t, true)
	v := decodeView(t, call(t, srv, http.MethodGet, "/api/backlog", ""))

	if len(v.Rows) != 2 || v.SweptAt == "" || v.Building {
		t.Errorf("rows %d swept_at %q building %v", len(v.Rows), v.SweptAt, v.Building)
	}
	if len(v.Sprints) != 2 || v.Sprints[0].State == "" {
		t.Errorf("sprints = %+v, want the board's active and future ones", v.Sprints)
	}
	if len(v.EpicsAll) != 1 || v.EpicsAll[0].Key != "ABC-100" {
		t.Errorf("epics_all = %+v", v.EpicsAll)
	}
	if len(v.Stories) != 1 || v.Stories[0].Key != "ABC-2" || v.Stories[0].EpicKey != "ABC-100" {
		t.Errorf("stories = %+v, want the open Story with its epic", v.Stories)
	}
	if keys := groupKeys(v, "no_story"); len(keys) != 1 || keys[0] != "ABC-1" {
		t.Errorf("no_story = %v", keys)
	}
	if v.Operations.LegacyLabelKeys == nil || len(v.Operations.LegacyLabelKeys) != 1 {
		t.Errorf("operations = %+v", v.Operations)
	}
	if v.Warnings == nil || len(v.Warnings) != 0 {
		t.Errorf("warnings = %#v, want an empty list", v.Warnings)
	}
	if v.Settings.OperationsLabel != "Operations" || v.Settings.WritesAllowed || v.Settings.AllowDelete {
		t.Errorf("settings = %+v", v.Settings)
	}
	// The row's points came through the board's field.
	for _, r := range v.Rows {
		if r.Key == "ABC-2" && (r.Points == nil || *r.Points != 5) {
			t.Errorf("ABC-2 points = %v", r.Points)
		}
	}
}

// Before the first sweep lands the page still answers, with every list
// a list, so the panel can render a "sweeping" state rather than crash.
func TestBacklogBeforeTheFirstSweep(t *testing.T) {
	srv := backlogServer(t, false)
	rec := call(t, srv, http.MethodGet, "/api/backlog", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{`"rows":[]`, `"groups":[`, `"sprints":[]`, `"epics_all":[]`, `"stories":[]`, `"warnings":[]`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the answer should carry %s, got %s", want, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("no list may be null: %s", rec.Body.String())
	}
}

func TestAcknowledgeHidesAndWithdrawShows(t *testing.T) {
	srv := backlogServer(t, true)
	rec := call(t, srv, http.MethodPost, "/api/backlog/ack", `{"keys":["ABC-1","ABC-999"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack: %d %s", rec.Code, rec.Body.String())
	}
	var ans struct {
		Acknowledged int      `json:"acknowledged"`
		Unknown      []string `json:"unknown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ans)
	if ans.Acknowledged != 1 || len(ans.Unknown) != 1 || ans.Unknown[0] != "ABC-999" {
		t.Errorf("ack answer = %+v", ans)
	}

	v := decodeView(t, call(t, srv, http.MethodGet, "/api/backlog", ""))
	if keys := groupKeys(v, "new_unacknowledged"); len(keys) != 0 {
		t.Errorf("ABC-1 should be hidden once acknowledged: %v", keys)
	}
	for _, r := range v.Rows {
		if r.Key == "ABC-1" && !r.Acknowledged {
			t.Error("the row should say it is acknowledged")
		}
	}

	if rec := call(t, srv, http.MethodDelete, "/api/backlog/ack", `{"keys":["ABC-1"]}`); rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body.String())
	}
	v = decodeView(t, call(t, srv, http.MethodGet, "/api/backlog", ""))
	if keys := groupKeys(v, "new_unacknowledged"); len(keys) != 1 || keys[0] != "ABC-1" {
		t.Errorf("ABC-1 should be back once withdrawn: %v", keys)
	}

	if rec := call(t, srv, http.MethodPost, "/api/backlog/ack", `{"keys":[]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("acknowledging nothing: status = %d, want 400", rec.Code)
	}
	if rec := call(t, srv, http.MethodPost, "/api/backlog/ack", `{"keys":`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unreadable body: status = %d, want 400", rec.Code)
	}
}

func TestInboxDismissAndUndo(t *testing.T) {
	srv := backlogServer(t, true)
	type inbox struct {
		Items    []backlog.Item `json:"items"`
		Warnings []string       `json:"warnings"`
	}
	read := func() inbox {
		rec := call(t, srv, http.MethodGet, "/api/backlog/inbox", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("inbox: %d %s", rec.Code, rec.Body.String())
		}
		var in inbox
		_ = json.Unmarshal(rec.Body.Bytes(), &in)
		return in
	}
	in := read()
	if len(in.Items) != 2 || in.Warnings == nil {
		t.Fatalf("inbox = %+v", in)
	}
	if in.Items[0].ID != "jira:mentioned:XYZ-9" || in.Items[1].ID != "confluence:mentioned:4242" {
		t.Errorf("items newest first = %s, %s", in.Items[0].ID, in.Items[1].ID)
	}
	if !strings.HasSuffix(in.Items[1].URL, "/wiki/spaces/DOCS/pages/4242") || in.Items[1].Summary != "page in Docs" {
		t.Errorf("confluence item = %+v", in.Items[1])
	}

	rec := call(t, srv, http.MethodPost, "/api/backlog/inbox/dismiss", `{"ids":["confluence:mentioned:4242"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body.String())
	}
	in = read()
	if in.Items[0].Dismissed || !in.Items[1].Dismissed {
		t.Errorf("only the page should be dismissed: %+v", in.Items)
	}
	if rec := call(t, srv, http.MethodDelete, "/api/backlog/inbox/dismiss", `{"ids":["confluence:mentioned:4242"]}`); rec.Code != http.StatusOK {
		t.Fatalf("undo: %d", rec.Code)
	}
	if in = read(); in.Items[1].Dismissed {
		t.Error("undoing should bring the page back")
	}
}

func TestOpsRoster(t *testing.T) {
	srv := backlogServer(t, true)
	type roster struct {
		Members    []map[string]any    `json:"members"`
		Candidates []backlog.Candidate `json:"candidates"`
	}
	read := func() roster {
		rec := call(t, srv, http.MethodGet, "/api/backlog/ops-roster", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("roster: %d %s", rec.Code, rec.Body.String())
		}
		var r roster
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		return r
	}
	r := read()
	if r.Members == nil || len(r.Members) != 0 || len(r.Candidates) != 2 || r.Candidates[0].Reported != 1 {
		t.Errorf("empty roster = %+v", r)
	}

	rec := call(t, srv, http.MethodPut, "/api/backlog/ops-roster", `{"members":[{"account_id":"acc-r1","name":"Reporter One"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	if r = read(); len(r.Members) != 1 || r.Members[0]["account_id"] != "acc-r1" {
		t.Errorf("after saving: %+v", r.Members)
	}
	// The roster is a signal: the reporter's ticket is now an operations
	// request missing the label.
	v := decodeView(t, call(t, srv, http.MethodGet, "/api/backlog", ""))
	if keys := v.Operations.MissingLabelKeys; len(keys) != 2 {
		t.Errorf("missing label = %v, want ABC-1 by its reporter beside ABC-2 by its legacy label", keys)
	}
	if len(v.Operations.Roster) != 1 || v.Operations.Roster[0].Label != "Reporter One" {
		t.Errorf("the view should carry the roster: %+v", v.Operations.Roster)
	}

	if rec := call(t, srv, http.MethodPut, "/api/backlog/ops-roster", `{"members":[{"name":"nobody"}]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a member without an account id: status = %d, want 400", rec.Code)
	}
}

func TestRefreshIsQueued(t *testing.T) {
	srv := backlogServer(t, true)
	if rec := call(t, srv, http.MethodPost, "/api/backlog/refresh", ""); rec.Code != http.StatusAccepted {
		t.Errorf("refresh: status = %d, want 202", rec.Code)
	}
}

func TestToolsListsTheBacklog(t *testing.T) {
	available := func(srv *server.Server) bool {
		rec := call(t, srv, http.MethodGet, "/api/tools", "")
		var body struct {
			Tools []struct {
				ID        string `json:"id"`
				Available bool   `json:"available"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		for _, tool := range body.Tools {
			if tool.ID == "backlog" {
				return tool.Available
			}
		}
		t.Fatal("no backlog entry in /api/tools")
		return false
	}
	if available(server.New(nil)) {
		t.Error("a server without the tool should say so")
	}
	if !available(backlogServer(t, false)) {
		t.Error("a server with the tool wired should say so")
	}
}
