package sprint

// These tests are in the package itself because what they check is the
// cache, which is deliberately not part of the exported surface.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
