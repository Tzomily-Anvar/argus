package server_test

// The close-out checklist, against the same stand-in Jira as the preview
// tests: one finished ticket with nothing filled in, one still open and
// being worked on. The stand-in fails the test if anything writes.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

// closeoutServer is previewServer with the store handed back, and the
// sprint already recorded in it: a draft is keyed by the sprint the way
// capacity is, and the store refuses one for a sprint it has never seen.
func closeoutServer(t *testing.T) (*server.Server, store.Store) {
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
	ctx := context.Background()
	if err := st.PutPerson(ctx, store.Person{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true}); err != nil {
		t.Fatal(err)
	}
	ends := time.Date(2026, 1, 19, 9, 0, 0, 0, time.UTC)
	if err := st.PutSprint(ctx, store.Sprint{JiraID: 744, Label: "Sprint 21", Number: 21, State: "closed", EndsAt: &ends}); err != nil {
		t.Fatal(err)
	}
	svc := sprint.NewService(client, st, sprint.Config{
		Project: "ABC", BaseURL: fake.URL,
		Rules: sprint.Rules{Done: []string{"Done"}, Container: []string{"Story"}},
	})
	srv := server.New(nil)
	srv.SprintRoutes(svc, st, nil)
	return srv, st
}

func TestCloseoutNeedsTheReportFirst(t *testing.T) {
	srv, _ := closeoutServer(t)
	rec := call(t, srv, http.MethodGet, "/api/sprint/closeout?sprint=21", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "open its report first") {
		t.Errorf("error = %q", body["error"])
	}
}

func TestCloseoutAnswersFromTheCachedReport(t *testing.T) {
	srv, _ := closeoutServer(t)
	if rec := call(t, srv, http.MethodGet, "/api/sprint/report?sprint=21", ""); rec.Code != http.StatusOK {
		t.Fatalf("building the report: %d %s", rec.Code, rec.Body.String())
	}

	rec := call(t, srv, http.MethodGet, "/api/sprint/closeout?sprint=21", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var m sprint.CloseoutModel
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if m.SprintJiraID != 744 || m.Sprint != 21 {
		t.Errorf("sprint = %d / %d", m.SprintJiraID, m.Sprint)
	}
	// ABC-1 finished with no points and nobody assigned, so it is in both
	// of the first two steps; with an empty changelog nobody is suggested,
	// and the roster is offered.
	if len(m.Size) != 1 || m.Size[0].Key != "ABC-1" || m.Size[0].Suggested != nil {
		t.Errorf("size = %+v", m.Size)
	}
	if len(m.Assign) != 1 || m.Assign[0].Key != "ABC-1" || m.Assign[0].SuggestedAccountID != "" {
		t.Errorf("assign = %+v", m.Assign)
	}
	if len(m.Assign) == 1 && (len(m.Assign[0].Candidates) != 1 || m.Assign[0].Candidates[0].AccountID != "acc-a") {
		t.Errorf("candidates = %+v, want the active roster", m.Assign[0].Candidates)
	}
	// ABC-2 is in progress and carried, so it is where effort is logged.
	if len(m.Effort) != 1 || m.Effort[0].Key != "ABC-2" || m.Effort[0].AssigneeAccountID != "acc-a" {
		t.Errorf("effort = %+v", m.Effort)
	}
	if m.Effort != nil && len(m.Effort) == 1 && m.Effort[0].Entries == nil {
		t.Error("entries must be an array, not null")
	}
	if !strings.Contains(rec.Body.String(), `"stories":[]`) {
		t.Errorf("no containers, so an empty array: %s", rec.Body.String())
	}
}
