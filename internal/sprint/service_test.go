package sprint

// These tests are in the package itself because what they check is the
// cache, which is deliberately not part of the exported surface.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

const testPointsField = "customfield_10016"

func testIssue(t *testing.T) jira.Issue {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"key": "ABC-1",
		"fields": map[string]any{
			"summary":       "Something",
			"issuetype":     map[string]any{"name": "Task"},
			"status":        map[string]any{"name": "Done"},
			"assignee":      map[string]any{"accountId": "acc-a", "displayName": "Person A"},
			"created":       "2026-01-01T09:00:00.000+0000",
			testPointsField: 5.0,
		},
	})
	if err != nil {
		t.Fatalf("encoding the test issue: %v", err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatalf("decoding the test issue: %v", err)
	}
	return is
}

// cachedService returns a service holding one already-built report, with
// no Jira client at all. A nil client is the point: anything that reaches
// for Jira here panics, which is how these tests prove that saving a
// number does not need a sweep.
func cachedService(t *testing.T) (*Service, jira.Sprint, store.Store) {
	t.Helper()

	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	ends := time.Date(2026, 1, 19, 17, 0, 0, 0, time.UTC)
	if err := st.PutSprint(ctx, store.Sprint{JiraID: 744, Label: "Sprint 21", Number: 21, EndsAt: &ends}); err != nil {
		t.Fatalf("seeding the sprint: %v", err)
	}
	if err := st.PutPerson(ctx, store.Person{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true}); err != nil {
		t.Fatalf("seeding the person: %v", err)
	}

	svc := NewService(nil, st, Config{
		Rules:            Rules{Done: []string{"Done"}, Container: []string{"Story"}},
		SprintLengthDays: 10,
	})

	sp := jira.Sprint{ID: 744, Name: "Sprint 21", Number: 21, State: "closed"}
	issues := []jira.Issue{testIssue(t)}
	svc.install(ctx, sp, issues, fetched{fields: &fields{points: testPointsField}}, time.Now().UTC())
	return svc, sp, st
}

func builtAt(t *testing.T, svc *Service, sprintID int64) time.Time {
	t.Helper()
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	entry := svc.reports[sprintID]
	if entry == nil {
		t.Fatal("expected a cached report")
	}
	return entry.builtAt
}

func report(t *testing.T, svc *Service, sprintID int64) Report {
	t.Helper()
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	entry := svc.reports[sprintID]
	if entry == nil {
		t.Fatal("expected a cached report")
	}
	return entry.report
}

// The editing bug: saving a day off used to throw the report away, so the
// next read paid a full Jira sweep and showed the old figure until it
// landed. The saved value must be in the cached report by the time the
// write returns.
func TestRecomputeFoldsInASavedCapacity(t *testing.T) {
	svc, sp, st := cachedService(t)
	ctx := context.Background()

	if got := report(t, svc, sp.ID).People[0].Capacity; got != 10 {
		t.Fatalf("starting capacity = %v, want the full baseline of 10", got)
	}

	if err := st.PutCapacity(ctx, store.Capacity{
		SprintJiraID: 744, AccountID: "acc-a", PlannedDaysOff: 2, Reviewed: true,
	}); err != nil {
		t.Fatalf("PutCapacity: %v", err)
	}
	svc.Recompute(ctx)

	rep := report(t, svc, sp.ID)
	if rep.People[0].Capacity != 8 {
		t.Errorf("capacity = %v, want 8 immediately after the save", rep.People[0].Capacity)
	}
	if rep.People[0].PlannedDaysOff != 2 {
		t.Errorf("planned days off = %v, want 2", rep.People[0].PlannedDaysOff)
	}
}

// A changed baseline is the same story, and the delivered points must
// survive the recomputation: the Jira half is not re-fetched, so if the
// issues were not kept the report would come back empty.
func TestRecomputeFoldsInASavedBaseline(t *testing.T) {
	svc, sp, st := cachedService(t)
	ctx := context.Background()

	if err := st.PutPerson(ctx, store.Person{
		AccountID: "acc-a", Name: "Person A", Baseline: 6, Active: true,
	}); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	svc.Recompute(ctx)

	rep := report(t, svc, sp.ID)
	if rep.People[0].Baseline != 6 {
		t.Errorf("baseline = %v, want 6", rep.People[0].Baseline)
	}
	if rep.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want the 5 points from the issues already held", rep.Summary.DeliveredTotal)
	}
}

// Marking a sprint reviewed is a write like any other, so it too has to
// reach the cached report at once.
func TestRecomputeFoldsInAReview(t *testing.T) {
	svc, sp, st := cachedService(t)
	ctx := context.Background()

	if got := report(t, svc, sp.ID).CapacityReview.State; got != ReviewNone {
		t.Fatalf("state = %q, want %q", got, ReviewNone)
	}

	if err := st.SetCapacityReviewed(ctx, 744, true); err != nil {
		t.Fatalf("SetCapacityReviewed: %v", err)
	}
	svc.Recompute(ctx)

	if got := report(t, svc, sp.ID).CapacityReview.State; got != ReviewNoAdjustments {
		t.Errorf("state = %q, want %q", got, ReviewNoAdjustments)
	}
}

// Recomputing fetches nothing, so it must not claim the Jira figures are
// any fresher than they were. The "as of" label is the promise that they
// are not live, and it is also what decides when the next real sweep runs.
func TestRecomputeKeepsTheBuildTime(t *testing.T) {
	svc, sp, st := cachedService(t)
	ctx := context.Background()

	was := builtAt(t, svc, sp.ID)
	if err := st.SetCapacityReviewed(ctx, 744, true); err != nil {
		t.Fatalf("SetCapacityReviewed: %v", err)
	}
	svc.Recompute(ctx)

	if now := builtAt(t, svc, sp.ID); !now.Equal(was) {
		t.Errorf("build time moved from %v to %v; nothing was fetched", was, now)
	}
}

// ---- what Jira declares --------------------------------------------
//
// A stand-in Jira that declares a project's statuses and one board's
// estimation, so the reads that turn configuration into conventions can
// be held to their rules: the done category fills the gap an override
// leaves, the board's field is taken only where the name lookup agrees
// or found nothing, and a failed read keeps the last reading.

const otherPointsField = "customfield_10026"

type declaring struct {
	t            *testing.T
	catalogue    []map[string]any // the field catalogue
	estimation   map[string]any   // the board's estimation block
	boardID      int64
	failStatuses bool
	requests     []string
}

func (d *declaring) serve(w http.ResponseWriter, r *http.Request) {
	d.requests = append(d.requests, r.Method+" "+r.URL.Path)
	answer := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	status := func(id, name, cat string) map[string]any {
		return map[string]any{"id": id, "name": name, "statusCategory": map[string]any{"key": cat}}
	}
	statuses := []any{status("1", "To Do", "new"), status("2", "In Progress", "indeterminate"),
		status("3", "Ready for production", "done"), status("4", "Done", "done")}
	onSprint := []map[string]any{{"id": 744, "name": "Sprint 21", "state": "closed"}}
	sprint := map[string]any{
		"id": 744, "name": "Sprint 21", "state": "closed", "originBoardId": d.boardID,
		"startDate": "2026-01-05T09:00:00.000+0000", "endDate": "2026-01-19T09:00:00.000+0000",
		"completeDate": "2026-01-19T10:00:00.000+0000",
	}
	issues := []map[string]any{
		{"id": "10001", "key": "ABC-1", "fields": map[string]any{
			"summary": "Shipped", "issuetype": map[string]any{"name": "Task"},
			"status": status("3", "Ready for production", "done"), "created": "2026-01-06T09:00:00.000+0000",
			"resolutiondate": "2026-01-10T09:00:00.000+0000",
			"assignee":       map[string]any{"accountId": "acc-a", "displayName": "Person A"},
			testPointsField:  5.0, otherPointsField: 4.0, "customfield_10020": onSprint,
		}},
		{"id": "10002", "key": "ABC-2", "fields": map[string]any{
			"summary": "Done too", "issuetype": map[string]any{"name": "Task"},
			"status": status("4", "Done", "done"), "created": "2026-01-06T09:00:00.000+0000",
			"resolutiondate": "2026-01-12T09:00:00.000+0000",
			"assignee":       map[string]any{"accountId": "acc-a", "displayName": "Person A"},
			testPointsField:  3.0, "customfield_10020": onSprint,
		}},
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /rest/api/3/field":
		answer(d.catalogue)
	case "GET /rest/api/3/status":
		answer(statuses)
	case "GET /rest/api/3/project/ABC/statuses":
		if d.failStatuses {
			http.Error(w, "gone away", http.StatusBadGateway)
			return
		}
		answer([]map[string]any{{"id": "1", "name": "Task", "statuses": statuses}})
	case "GET /rest/agile/1.0/sprint/744":
		answer(sprint)
	case "GET /rest/agile/1.0/board/7/configuration":
		answer(map[string]any{"id": 7, "estimation": d.estimation, "columnConfig": map[string]any{
			"columns": []map[string]any{
				{"name": "To Do", "statuses": []map[string]any{{"id": "1"}}},
				{"name": "In Progress", "statuses": []map[string]any{{"id": "2"}}},
				{"name": "Done", "statuses": []map[string]any{{"id": "3"}, {"id": "4"}}},
			}}})
	case "GET /rest/agile/1.0/board/7/sprint":
		answer(map[string]any{"values": []any{sprint}})
	case "POST /rest/api/3/search/jql":
		answer(map[string]any{"issues": issues, "isLast": true})
	case "POST /rest/api/3/changelog/bulkfetch":
		answer(map[string]any{"issueChangeLogs": []any{}})
	default:
		d.t.Errorf("the stand-in got %s %s", r.Method, r.URL)
		http.Error(w, "not here", http.StatusNotFound)
	}
}

func (d *declaring) saw(path string) bool {
	for _, r := range d.requests {
		if strings.HasSuffix(r, path) {
			return true
		}
	}
	return false
}

// declaringService wires a service to the stand-in, with the registry of
// declared values kept in a temporary directory.
func declaringService(t *testing.T, d *declaring, done []string) *Service {
	t.Helper()
	d.t = t
	if d.catalogue == nil {
		d.catalogue = []map[string]any{
			{"id": testPointsField, "name": "Story Points", "custom": true},
			{"id": "customfield_10020", "name": "Sprint", "custom": true},
		}
	}
	if d.estimation == nil {
		d.estimation = map[string]any{"type": "field", "field": map[string]any{
			"fieldId": testPointsField, "displayName": "Story Points"}}
	}
	srv := httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(srv.Close)
	t.Setenv("ARGUS_JIRA_POINTS_FIELD", "")
	t.Setenv("ARGUS_JIRA_POINTS_FIELD_NAME", "")
	config.UseDeclaredFile(filepath.Join(t.TempDir(), "declared.json"))
	t.Cleanup(func() { config.UseDeclaredFile("") })

	client, err := jira.New(srv.URL, "operator@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewService(client, st, Config{
		Project: "ABC", BaseURL: srv.URL,
		Rules:          Rules{Done: done, Container: []string{"Story"}},
		StoryLinkTypes: []string{"Blocks"},
	})
}

func built(t *testing.T, svc *Service, force bool) (Report, *fields) {
	t.Helper()
	res, err := svc.Report(context.Background(), 21, force)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return res.Report, svc.reports[744].fields
}

func origin(t *testing.T, key string) config.Resolution {
	t.Helper()
	s, ok := config.Core().Find(key)
	if !ok {
		t.Fatalf("%s is not in the catalogue", key)
	}
	return s.Resolve(nil)
}

func assumed(rep Report, kind string) (Flag, bool) {
	for _, f := range rep.Assumptions {
		if f.Kind == kind {
			return f, true
		}
	}
	return Flag{}, false
}

func TestDoneSetIsReadFromTheStatusCategories(t *testing.T) {
	d := &declaring{boardID: 7}
	svc := declaringService(t, d, nil)
	rep, f := built(t, svc, false)

	if got := strings.Join(f.done, ","); got != "Ready for production,Done" {
		t.Errorf("done set = %q, want every done-category status", got)
	}
	if rep.Summary.DoneCount != 2 || rep.Summary.DeliveredTotal != 8 {
		t.Errorf("done = %d, delivered = %v; both statuses count", rep.Summary.DoneCount, rep.Summary.DeliveredTotal)
	}
	for key, source := range map[string]string{
		"ARGUS_JIRA_DONE_STATUSES":      "status categories",
		"ARGUS_JIRA_POINTS_FIELD":       "board estimation",
		"ARGUS_JIRA_SPRINT_LENGTH_DAYS": "sprint dates",
	} {
		r := origin(t, key)
		if r.Origin != config.FromJira || r.Declared == nil || r.Declared.Source != source {
			t.Errorf("%s resolves from %s (%+v), want from Jira via %q", key, r.Origin, r.Declared, source)
		}
	}
	if v := origin(t, "ARGUS_JIRA_SPRINT_LENGTH_DAYS").Value; v != "10" {
		t.Errorf("declared sprint length = %q, want the 10 working days the sprint ran", v)
	}
	if f.points != testPointsField || !f.declared.PointsFromJira {
		t.Errorf("points = %q from Jira %v; the board and the name agree, so the board's is in force", f.points, f.declared.PointsFromJira)
	}
}

func TestDoneOverrideReplacesTheDeclaredList(t *testing.T) {
	d := &declaring{boardID: 7}
	svc := declaringService(t, d, []string{"Done"})
	rep, f := built(t, svc, false)

	if got := strings.Join(f.done, ","); got != "Done" {
		t.Errorf("done set = %q, want the override alone", got)
	}
	if rep.Summary.DoneCount != 1 {
		t.Errorf("done = %d, want only the issue in the overriding status", rep.Summary.DoneCount)
	}
	if d.saw("/project/ABC/statuses") {
		t.Error("the project's statuses were read although the override replaces them")
	}
	if r := origin(t, "ARGUS_JIRA_DONE_STATUSES"); r.Origin == config.FromJira {
		t.Error("an overridden setting must not be reported as declared by Jira")
	}
}

func TestBoardFieldIsTakenWhereTheNameFindsNothing(t *testing.T) {
	d := &declaring{boardID: 7, catalogue: []map[string]any{{"id": "customfield_10020", "name": "Sprint", "custom": true}}}
	svc := declaringService(t, d, nil)
	rep, f := built(t, svc, false)

	if f.points != testPointsField || !f.declared.PointsFromJira {
		t.Errorf("points = %q from Jira %v, want the board's field", f.points, f.declared.PointsFromJira)
	}
	if rep.Summary.DeliveredTotal != 8 {
		t.Errorf("delivered = %v, want 8 read through the board's field", rep.Summary.DeliveredTotal)
	}
}

func TestBoardFieldIsHeldBackWhereTheNameDisagrees(t *testing.T) {
	d := &declaring{boardID: 7, estimation: map[string]any{"type": "field", "field": map[string]any{
		"fieldId": otherPointsField, "displayName": "Story point estimate"}}}
	svc := declaringService(t, d, nil)
	rep, f := built(t, svc, false)

	if f.points != testPointsField || f.declared.PointsFromJira {
		t.Errorf("points = %q from Jira %v, want the named field kept", f.points, f.declared.PointsFromJira)
	}
	a, ok := assumed(rep, AssumePointsFieldDisagrees)
	if !ok {
		t.Fatalf("no assumption about the disagreement; got %v", rep.Assumptions)
	}
	for _, want := range []string{otherPointsField + " (Story point estimate), which 1 issue", testPointsField + ", which 2 issues",
		"ARGUS_JIRA_POINTS_FIELD=" + otherPointsField} {
		if !strings.Contains(a.Message, want) {
			t.Errorf("message %q does not say %q", a.Message, want)
		}
	}
	if r := origin(t, "ARGUS_JIRA_POINTS_FIELD"); r.Origin == config.FromJira {
		t.Error("a held-back field must not be reported as in force from Jira")
	}
}

func TestBoardEstimatingByIssueCountFallsBackToTheName(t *testing.T) {
	d := &declaring{boardID: 7, estimation: map[string]any{"type": "issueCount"}}
	svc := declaringService(t, d, nil)
	rep, f := built(t, svc, false)

	if f.points != testPointsField || f.declared.PointsFromJira {
		t.Errorf("points = %q from Jira %v, want the name lookup", f.points, f.declared.PointsFromJira)
	}
	if _, ok := assumed(rep, AssumeIssueCountBoard); !ok {
		t.Errorf("no assumption says the board estimates by issue count; got %v", rep.Assumptions)
	}
}

func TestAFailedReadKeepsTheLastReadingAndSaysSo(t *testing.T) {
	d := &declaring{boardID: 7}
	svc := declaringService(t, d, nil)
	if _, f := built(t, svc, false); f.declared.Stale {
		t.Fatal("the first reading is not stale")
	}

	d.failStatuses = true
	rep, f := built(t, svc, true)
	if got := strings.Join(f.done, ","); got != "Ready for production,Done" || !f.declared.Stale {
		t.Errorf("after the failure: done = %q, stale = %v; want the last reading, marked stale", got, f.declared.Stale)
	}
	if _, ok := assumed(rep, AssumeStale); !ok {
		t.Errorf("no assumption says the conventions are stale; got %v", rep.Assumptions)
	}
	if r := origin(t, "ARGUS_JIRA_DONE_STATUSES"); r.Declared == nil || !r.Declared.Stale {
		t.Errorf("the registry does not say the value is stale: %+v", r.Declared)
	}
}

func TestNoBoardFallsBackToTheNameWithAWarning(t *testing.T) {
	d := &declaring{boardID: 0}
	svc := declaringService(t, d, nil)
	rep, f := built(t, svc, false)

	if f.points != testPointsField || f.declared.PointsFromJira {
		t.Errorf("points = %q from Jira %v, want the name lookup", f.points, f.declared.PointsFromJira)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "no board") {
		t.Errorf("warnings = %q, want one saying the sprint has no board", rep.Warnings)
	}
}
