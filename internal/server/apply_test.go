package server_test

// The apply against a stand-in Jira that accepts the writes the gate
// lists and records exactly what it was sent. Where changes_test.go
// holds that a preview never writes, these hold what an apply writes:
// nothing with writes off, exactly the previewed request with them on,
// nothing where Jira has moved, and nothing further once the token has
// been refused three times.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

type sent struct {
	Method, Path, Query, Body string
}

// standIn is a Jira that serves the same closed sprint as fakeJira and
// also takes writes. It keeps the points it is sent, so a re-read after
// a write sees what Jira would hold, and answers every write with one
// status when told to fail.
type standIn struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	writes  []sent
	points  map[string]any // by key, what the last edit put there
	updated string         // what a re-read says fields.updated is
	status  int            // the answer to every write; 0 means success
}

const previewUpdated = "2026-01-18T09:00:00.000+0000"

func newStandIn(t *testing.T) *standIn {
	t.Helper()
	f := &standIn{t: t, points: map[string]any{}, updated: previewUpdated}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// issue is one of the two issues, as the search and a re-read return it.
func (f *standIn) issue(id, key string, updated string) map[string]any {
	onSprint := []map[string]any{{"id": 744, "name": "Sprint 21", "state": "closed"}}
	fields := map[string]any{
		"issuetype": map[string]any{"name": "Task"}, "created": "2026-01-06T09:00:00.000+0000",
		"updated": updated, fakeSprintField: onSprint,
	}
	if key == "ABC-1" {
		fields["summary"] = "Finished, unsized"
		fields["status"] = map[string]any{"name": "Done"}
		fields["resolutiondate"] = "2026-01-10T09:00:00.000+0000"
	} else {
		fields["summary"] = "Still open"
		fields["status"] = map[string]any{"name": "In Progress"}
		fields["assignee"] = map[string]any{"accountId": "acc-a", "displayName": "Person A"}
	}
	if p, ok := f.points[key]; ok && p != nil {
		fields[fakePointsField] = p
	}
	return map[string]any{"id": id, "key": key, "fields": fields}
}

func (f *standIn) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if v != nil {
			_ = json.NewEncoder(w).Encode(v)
		}
	}
	body, _ := io.ReadAll(r.Body)
	switch r.Method + " " + r.URL.Path {
	case "GET /rest/api/3/field":
		answer(200, []map[string]any{
			{"id": fakePointsField, "name": "Story Points", "custom": true},
			{"id": fakeSprintField, "name": "Sprint", "custom": true},
		})
	case "GET /rest/api/3/status":
		answer(200, []any{})
	case "GET /rest/agile/1.0/sprint/744":
		answer(200, map[string]any{
			"id": 744, "name": "Sprint 21", "state": "closed", "originBoardId": 0,
			"startDate": "2026-01-05T09:00:00.000+0000", "endDate": "2026-01-19T09:00:00.000+0000",
			"completeDate": "2026-01-19T10:00:00.000+0000",
		})
	case "POST /rest/api/3/search/jql":
		answer(200, map[string]any{"issues": []any{
			f.issue("10001", "ABC-1", previewUpdated), f.issue("10002", "ABC-2", previewUpdated),
		}, "isLast": true})
	case "POST /rest/api/3/changelog/bulkfetch":
		answer(200, map[string]any{"issueChangeLogs": []any{}})
	case "GET /rest/api/3/issue/10001":
		answer(200, f.issue("10001", "ABC-1", f.updated))
	case "GET /rest/api/3/issue/10002":
		answer(200, f.issue("10002", "ABC-2", f.updated))
	case "GET /rest/api/3/issue/ABC-1/editmeta", "GET /rest/api/3/issue/ABC-2/editmeta":
		answer(200, map[string]any{"fields": map[string]any{fakePointsField: map[string]any{}, "assignee": map[string]any{}}})
	case "PUT /rest/api/3/issue/ABC-1", "PUT /rest/api/3/issue/ABC-2", "POST /rest/api/3/issue/ABC-2/worklog":
		f.writes = append(f.writes, sent{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		if f.status != 0 {
			answer(f.status, map[string]any{"errorMessages": []string{"no"}})
			return
		}
		if r.Method == http.MethodPost {
			answer(201, map[string]any{"id": "46"})
			return
		}
		var edit struct {
			Fields map[string]any `json:"fields"`
		}
		_ = json.Unmarshal(body, &edit)
		if v, ok := edit.Fields[fakePointsField]; ok {
			f.points[strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")] = v
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("the stand-in Jira got %s %s, which nothing here should make", r.Method, r.URL)
		http.Error(w, "not here", http.StatusNotFound)
	}
}

func (f *standIn) sent() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent{}, f.writes...)
}

// writableServer wires a server the way cmd/argus does, with the setting
// read once into the service. The environment variable is set too, so
// the status endpoint agrees with the service.
func writableServer(t *testing.T, fake *standIn, enable bool) (*server.Server, store.Store) {
	t.Helper()
	if enable {
		t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "1")
	} else {
		t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "0")
	}
	client, err := jira.New(fake.srv.URL, "operator@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.PutPerson(t.Context(), store.Person{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true}); err != nil {
		t.Fatal(err)
	}
	svc := sprint.NewService(client, st, sprint.Config{
		Project: "ABC", BaseURL: fake.srv.URL,
		Rules:        sprint.Rules{Done: []string{"Done"}, Container: []string{"Story"}},
		Actor:        "operator@example.com",
		EnableWrites: enable,
	})
	srv := server.New(nil)
	srv.SprintRoutes(svc, st)
	return srv, st
}

// preview opens the report and builds a change set from the requests.
func preview(t *testing.T, srv *server.Server, changes string) sprint.ChangeSet {
	t.Helper()
	if rec := call(t, srv, http.MethodGet, "/api/sprint/report?sprint=21", ""); rec.Code != http.StatusOK {
		t.Fatalf("building the report: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(t, srv, http.MethodPost, "/api/sprint/changes", `{"sprint":21,"changes":[`+changes+`]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var cs sprint.ChangeSet
	if err := json.Unmarshal(rec.Body.Bytes(), &cs); err != nil {
		t.Fatal(err)
	}
	return cs
}

func apply(t *testing.T, srv *server.Server, id, digest string) (*httptest.ResponseRecorder, sprint.Result) {
	t.Helper()
	rec := call(t, srv, http.MethodPost, "/api/sprint/changes/"+id+"/apply", `{"digest":"`+digest+`"}`)
	var res sprint.Result
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec, res
}

func writesLogged(t *testing.T, srv *server.Server, query string) []store.WriteRecord {
	t.Helper()
	rec := call(t, srv, http.MethodGet, "/api/sprint/writes"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET writes: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Writes []store.WriteRecord `json:"writes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Writes == nil {
		t.Error("writes must be an array, not null")
	}
	return body.Writes
}

const pointsOnABC1 = `{"key":"ABC-1","op":"points.set","points":3}`

func TestApplyIsRefusedWhileWritesAreOff(t *testing.T) {
	fake := newStandIn(t)
	srv, _ := writableServer(t, fake, false)
	cs := preview(t, srv, pointsOnABC1)

	rec, _ := apply(t, srv, cs.ID, cs.Digest)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Fatalf("status = %d, want 403 naming the setting; body %s", rec.Code, rec.Body.String())
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("the stand-in saw writes with the setting off: %+v", got)
	}
	if rec := call(t, srv, http.MethodGet, "/api/sprint/changes/"+cs.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("the preview should still be held after a refusal: %d", rec.Code)
	}
	if got := writesLogged(t, srv, ""); len(got) != 0 {
		t.Errorf("a refusal logged %d rows", len(got))
	}
}

func TestApplyWritesExactlyWhatWasPreviewedOnce(t *testing.T) {
	fake := newStandIn(t)
	srv, _ := writableServer(t, fake, true)
	cs := preview(t, srv, pointsOnABC1)

	if rec, _ := apply(t, srv, cs.ID, "not-the-digest"); rec.Code != http.StatusConflict {
		t.Fatalf("wrong digest: status = %d, want 409", rec.Code)
	}
	if rec, _ := apply(t, srv, "no-such-set", cs.Digest); rec.Code != http.StatusConflict {
		t.Fatalf("unknown id: status = %d, want 409", rec.Code)
	}
	if got := fake.sent(); len(got) != 0 {
		t.Fatalf("a refused apply wrote: %+v", got)
	}

	rec, res := apply(t, srv, cs.ID, cs.Digest)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if res.Applied != 1 || res.Skipped != 0 || res.Failed != 0 || res.Stopped != "" || len(res.Rows) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if row := res.Rows[0]; row.Key != "ABC-1" || row.Op != "points.set" || row.Outcome != store.OutcomeApplied || row.AfterLabel != "3" {
		t.Errorf("row = %+v", row)
	}
	want := []sent{{http.MethodPut, "/rest/api/3/issue/ABC-1", "", `{"fields":{"` + fakePointsField + `":3}}`}}
	if got := fake.sent(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("the stand-in saw %+v, want exactly %+v", got, want)
	}

	logged := writesLogged(t, srv, "?limit=100")
	if len(logged) != 1 {
		t.Fatalf("audit rows = %d, want 1: %+v", len(logged), logged)
	}
	w := logged[0]
	if w.Operation != "points.set" || w.Target != "ABC-1" || w.Before != "" || w.After != "3" ||
		w.Actor != "operator@example.com" || w.ChangeSet != cs.ID || w.Outcome != store.OutcomeApplied ||
		!strings.Contains(w.Note, "digest "+cs.Digest) || !strings.Contains(w.Note, "finished with no estimate") {
		t.Errorf("audit row = %+v", w)
	}

	// Single use: the set is gone, and so is its preview.
	if rec, _ := apply(t, srv, cs.ID, cs.Digest); rec.Code != http.StatusConflict {
		t.Errorf("second apply: status = %d, want 409", rec.Code)
	}
	if rec := call(t, srv, http.MethodGet, "/api/sprint/changes/"+cs.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("an applied preview must be gone: %d", rec.Code)
	}

	// The reversal is a new preview built from the log, and applying it
	// clears the field through the same gate, guarded on the 3 we wrote.
	rec = call(t, srv, http.MethodPost, "/api/sprint/changes/reverse", `{"change_set":"`+cs.ID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reverse: %d %s", rec.Code, rec.Body.String())
	}
	var back sprint.ChangeSet
	_ = json.Unmarshal(rec.Body.Bytes(), &back)
	if len(back.Changes) != 1 || back.Changes[0].After != nil || back.Changes[0].Guard.Was != 3.0 || back.Changes[0].Guard.IssueID != "10001" {
		t.Fatalf("reversal = %+v", back.Changes)
	}
	if got := fake.sent(); len(got) != 1 {
		t.Fatalf("building the reversal wrote something: %+v", got[1:])
	}
	rec, res = apply(t, srv, back.ID, back.Digest)
	if rec.Code != http.StatusOK || res.Applied != 1 {
		t.Fatalf("applying the reversal: %d %s", rec.Code, rec.Body.String())
	}
	got := fake.sent()
	if len(got) != 2 || got[1] != (sent{http.MethodPut, "/rest/api/3/issue/ABC-1", "", `{"fields":{"` + fakePointsField + `":null}}`}) {
		t.Errorf("the reversal sent %+v", got)
	}
	if logged = writesLogged(t, srv, "?limit=1"); len(logged) != 1 || logged[0].Before != "3" || logged[0].After != "" || logged[0].ChangeSet != back.ID {
		t.Errorf("the reversal's row = %+v", logged)
	}
	if rec = call(t, srv, http.MethodPost, "/api/sprint/changes/reverse", `{"change_set":"nothing"}`); rec.Code != http.StatusNotFound {
		t.Errorf("reversing an unknown set: %d, want 404", rec.Code)
	}
}

func TestApplySkipsWhenJiraMovedUnderneath(t *testing.T) {
	fake := newStandIn(t)
	srv, _ := writableServer(t, fake, true)
	cs := preview(t, srv, pointsOnABC1)

	fake.mu.Lock()
	fake.updated = "2026-01-18T09:30:00.000+0000"
	fake.mu.Unlock()

	rec, res := apply(t, srv, cs.ID, cs.Digest)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if res.Skipped != 1 || res.Applied != 0 || len(res.Rows) != 1 || res.Rows[0].Outcome != store.OutcomeSkipped ||
		!strings.Contains(res.Rows[0].Reason, "since the preview") {
		t.Errorf("result = %+v", res)
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("a conflict must not write, but the stand-in saw %+v", got)
	}
	logged := writesLogged(t, srv, "")
	if len(logged) != 1 || logged[0].Outcome != store.OutcomeSkipped || !strings.Contains(logged[0].Note, "skipped: edited in Jira") {
		t.Errorf("a skip is recorded too: %+v", logged)
	}
}

func TestApplyStopsAfterThreePermissionFailures(t *testing.T) {
	fake := newStandIn(t)
	srv, _ := writableServer(t, fake, true)
	cs := preview(t, srv, strings.Join([]string{
		pointsOnABC1,
		`{"key":"ABC-1","op":"assignee.set","assignee":"acc-a"}`,
		`{"key":"ABC-2","op":"points.set","points":2}`,
		`{"key":"ABC-2","op":"worklog.add","person":"acc-a","hours":6}`,
	}, ","))
	if len(cs.Changes) != 4 {
		t.Fatalf("want four changes to apply, got %d: %+v", len(cs.Changes), cs.Skipped)
	}

	fake.mu.Lock()
	fake.status = http.StatusForbidden
	fake.mu.Unlock()

	rec, res := apply(t, srv, cs.ID, cs.Digest)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if res.Failed != 3 || res.Skipped != 1 || res.Applied != 0 || !strings.Contains(res.Stopped, "three consecutive permission failures") {
		t.Errorf("result = %+v", res)
	}
	if last := res.Rows[3]; last.Outcome != store.OutcomeSkipped || last.Reason != "not attempted" || last.Op != "worklog.add" {
		t.Errorf("the fourth row = %+v", last)
	}
	if got := fake.sent(); len(got) != 3 {
		t.Errorf("the stand-in saw %d writes, want the three that were refused", len(got))
	}
	logged := writesLogged(t, srv, "")
	if len(logged) != 3 {
		t.Fatalf("audit rows = %d, want one per attempt and none for the row not attempted", len(logged))
	}
	for _, w := range logged {
		if w.Outcome != store.OutcomeFailed || w.ChangeSet != cs.ID || !strings.Contains(w.Note, "failed: ") {
			t.Errorf("row = %+v", w)
		}
	}
}
