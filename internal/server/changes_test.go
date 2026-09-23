package server_test

// The preview endpoints against a stand-in Jira that serves one closed
// sprint and fails the test if anything tries to write to it. That is
// the property worth holding: a preview is built from what the report
// already fetched, and no request it makes is a write.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

const (
	fakeSprintField = "customfield_10020"
	fakePointsField = "customfield_10016"
)

// fakeJira answers the handful of reads a sprint sweep makes. One sprint,
// two issues: a finished task with nothing filled in, and one still open.
func fakeJira(t *testing.T) *httptest.Server {
	t.Helper()
	onSprint := []map[string]any{{"id": 744, "name": "Sprint 21", "state": "closed"}}
	issues := []map[string]any{
		{"id": "10001", "key": "ABC-1", "fields": map[string]any{
			"summary": "Finished, unsized", "issuetype": map[string]any{"name": "Task"},
			"status": map[string]any{"name": "Done"}, "created": "2026-01-06T09:00:00.000+0000",
			"updated": "2026-01-18T09:00:00.000+0000", "resolutiondate": "2026-01-10T09:00:00.000+0000",
			fakeSprintField: onSprint,
		}},
		{"id": "10002", "key": "ABC-2", "fields": map[string]any{
			"summary": "Still open", "issuetype": map[string]any{"name": "Task"},
			"status": map[string]any{"name": "In Progress"}, "created": "2026-01-06T09:00:00.000+0000",
			"updated":       "2026-01-18T09:00:00.000+0000",
			"assignee":      map[string]any{"accountId": "acc-a", "displayName": "Person A"},
			fakeSprintField: onSprint,
		}},
	}
	answer := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/3/field":
			answer(w, []map[string]any{
				{"id": fakePointsField, "name": "Story Points", "custom": true},
				{"id": fakeSprintField, "name": "Sprint", "custom": true},
			})
		case "GET /rest/api/3/status":
			answer(w, []any{})
		case "GET /rest/agile/1.0/sprint/744":
			answer(w, map[string]any{
				"id": 744, "name": "Sprint 21", "state": "closed", "originBoardId": 0,
				"startDate": "2026-01-05T09:00:00.000+0000", "endDate": "2026-01-19T09:00:00.000+0000",
				"completeDate": "2026-01-19T10:00:00.000+0000",
			})
		case "POST /rest/api/3/search/jql":
			answer(w, map[string]any{"issues": issues, "isLast": true})
		case "POST /rest/api/3/changelog/bulkfetch":
			answer(w, map[string]any{"issueChangeLogs": []any{}})
		default:
			t.Errorf("the stand-in Jira got %s %s, which a preview must never make", r.Method, r.URL)
			http.Error(w, "not here", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func previewServer(t *testing.T) *server.Server {
	t.Helper()
	fake := fakeJira(t)
	client, err := jira.New(fake.URL, "operator@example.com", "token", 1, 5)
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
		Project: "ABC", BaseURL: fake.URL,
		Rules: sprint.Rules{Done: []string{"Done"}, Container: []string{"Story"}},
	})
	srv := server.New(nil)
	srv.SprintRoutes(svc, st)
	return srv
}

func call(t *testing.T, srv *server.Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestPreviewNeedsTheReportFirst(t *testing.T) {
	srv := previewServer(t)
	rec := call(t, srv, http.MethodPost, "/api/sprint/changes", `{"sprint":21,"changes":[{"key":"ABC-1","op":"points.set","points":3}]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "open its report first") {
		t.Errorf("error = %q", body["error"])
	}
}

func TestPreviewRejectsABodyItCannotRead(t *testing.T) {
	srv := previewServer(t)
	if rec := call(t, srv, http.MethodPost, "/api/sprint/changes", `{"sprint":`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// With writes off the preview is still built and still served. That is
// the whole point of the setting being separate from the preview: the
// proposals have to be judgeable before anybody switches writing on.
func TestPreviewWithWritesOffStillAnswers(t *testing.T) {
	t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "0")
	srv := previewServer(t)

	if rec := call(t, srv, http.MethodGet, "/api/sprint/report?sprint=21", ""); rec.Code != http.StatusOK {
		t.Fatalf("building the report: %d %s", rec.Code, rec.Body.String())
	}

	rec := call(t, srv, http.MethodPost, "/api/sprint/changes", `{"sprint":21,"changes":[
		{"key":"ABC-1","op":"points.set","points":3},
		{"key":"ABC-1","op":"assignee.set","assignee":"acc-a"},
		{"key":"ABC-2","op":"worklog.add","person":"acc-a","hours":6},
		{"key":"ABC-2","op":"points.set","points":2},
		{"key":"ABC-1","op":"worklog.add","person":"acc-a","hours":6}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var cs sprint.ChangeSet
	if err := json.Unmarshal(rec.Body.Bytes(), &cs); err != nil {
		t.Fatalf("decoding the change set: %v", err)
	}
	if cs.ID == "" || cs.Digest == "" || cs.SprintJiraID != 744 {
		t.Errorf("change set header = id %q digest %q sprint %d", cs.ID, cs.Digest, cs.SprintJiraID)
	}
	if len(cs.Changes) != 4 || len(cs.Skipped) != 1 {
		t.Fatalf("want four changes and one skip, got %d and %d: %+v", len(cs.Changes), len(cs.Skipped), cs.Skipped)
	}
	if cs.Changes[0].Field != fakePointsField || cs.Changes[0].Guard.IssueID != "10001" {
		t.Errorf("the points change should name the site's field and the issue id: %+v", cs.Changes[0])
	}
	if !strings.Contains(cs.Skipped[0].Reason, "not open when the sprint closed") {
		t.Errorf("skip = %+v", cs.Skipped[0])
	}

	// A reload finds the same preview under its id.
	again := call(t, srv, http.MethodGet, "/api/sprint/changes/"+cs.ID, "")
	if again.Code != http.StatusOK {
		t.Fatalf("GET by id: %d %s", again.Code, again.Body.String())
	}
	var held sprint.ChangeSet
	_ = json.Unmarshal(again.Body.Bytes(), &held)
	if held.Digest != cs.Digest {
		t.Errorf("the held preview digests to %q, the answer to %q", held.Digest, cs.Digest)
	}

	if rec := call(t, srv, http.MethodGet, "/api/sprint/changes/not-an-id", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown id: status = %d, want 404", rec.Code)
	}
}

func TestWritesStatusReportsTheSetting(t *testing.T) {
	t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "0")
	srv := previewServer(t)
	rec := call(t, srv, http.MethodGet, "/api/sprint/writes/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Allowed bool   `json:"allowed"`
		Setting string `json:"setting"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Allowed {
		t.Error("writes are off and the status says they are on")
	}
	if body.Setting != "ARGUS_SPRINT_ALLOW_WRITES" || !strings.Contains(body.Reason, "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Errorf("the answer must name the setting: %+v", body)
	}

	t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "1")
	rec = call(t, srv, http.MethodGet, "/api/sprint/writes/status", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Allowed {
		t.Error("writes are on and the status says they are off")
	}
}
