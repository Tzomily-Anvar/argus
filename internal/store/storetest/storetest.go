// Package storetest is the conformance suite every Store backend must
// pass.
//
// Two backends implement the same interface for different reasons - files
// for a zero-setup default, Postgres for years of history - and the only
// thing keeping them behaving identically is that both run these tests.
// A behaviour asserted here is part of the contract; a backend free to
// differ on it does not belong behind this interface.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Run exercises a backend. `fresh` must return an empty store.
func Run(t *testing.T, fresh func(t *testing.T) store.Store) {
	t.Helper()
	tests := map[string]func(*testing.T, store.Store){
		"people round-trip":                testPeople,
		"person upsert replaces":           testPersonUpsert,
		"inactive people are filterable":   testInactivePeople,
		"missing person is ErrNotFound":    testMissingPerson,
		"sprints keyed by Jira id":         testSprints,
		"a page id survives a re-sweep":    testPageIDSurvivesSweep,
		"capacity is isolated per sprint":  testCapacityIsolation,
		"absence splits planned/unplanned": testAbsenceSplit,
		"capacity needs known references":  testUnknownReference,
		"a sprint can be reviewed":         testCapacityReview,
		"a review survives a re-sweep":     testReviewSurvivesSweep,
		"retention keeps aggregates":       testRetention,
		"stats round-trip":                 testStats,
		"writes are append-only":           testWriteAudit,
		"a draft round-trips":              testDraft,
		"a draft is replaced whole":        testDraftReplaced,
		"a draft can be discarded":         testDraftDiscarded,
		"a draft needs a known sprint":     testDraftUnknownSprint,
		"empty store reads as empty":       testEmptyStore,
		"migrate is repeatable":            testMigrateIsRepeatable,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, fresh(t)) })
	}
}

func ctx() context.Context { return context.Background() }

// seed creates the sprint and person that capacity rows reference. Both
// backends enforce those references - Postgres with foreign keys, the
// file backend explicitly - so a test writing capacity must create them.
func seed(t *testing.T, s store.Store, sprintID int64, accountIDs ...string) {
	t.Helper()
	ends := time.Now().UTC().Add(-24 * time.Hour)
	if err := s.PutSprint(ctx(), store.Sprint{
		JiraID: sprintID, Label: "Sprint X", Number: int(sprintID), EndsAt: &ends,
	}); err != nil {
		t.Fatalf("seed sprint: %v", err)
	}
	for _, id := range accountIDs {
		if err := s.PutPerson(ctx(), store.Person{
			AccountID: id, Name: "Person " + id, Baseline: 10, Active: true,
		}); err != nil {
			t.Fatalf("seed person: %v", err)
		}
	}
}

func testPeople(t *testing.T, s store.Store) {
	p := store.Person{AccountID: "acc-1", Name: "Ada", Baseline: 10, Active: true}
	if err := s.PutPerson(ctx(), p); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	got, err := s.GetPerson(ctx(), "acc-1")
	if err != nil {
		t.Fatalf("GetPerson: %v", err)
	}
	if got.Name != "Ada" || got.Baseline != 10 || !got.Active {
		t.Errorf("round-trip lost data: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be stamped on write")
	}
}

// An account id identifies a person, so storing one twice must update
// rather than duplicate - otherwise a renamed colleague becomes two.
func testPersonUpsert(t *testing.T, s store.Store) {
	_ = s.PutPerson(ctx(), store.Person{AccountID: "acc-1", Name: "Ada", Baseline: 10, Active: true})
	_ = s.PutPerson(ctx(), store.Person{AccountID: "acc-1", Name: "Ada L", Baseline: 8, Active: true})

	people, err := s.ListPeople(ctx(), true)
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	if len(people) != 1 {
		t.Fatalf("expected 1 person after upsert, got %d", len(people))
	}
	if people[0].Name != "Ada L" || people[0].Baseline != 8 {
		t.Errorf("upsert did not replace: %+v", people[0])
	}
}

// Someone who has left stops appearing in new sprints, but their history
// must survive - so they are deactivated, never deleted.
func testInactivePeople(t *testing.T, s store.Store) {
	_ = s.PutPerson(ctx(), store.Person{AccountID: "a", Name: "Active", Baseline: 10, Active: true})
	_ = s.PutPerson(ctx(), store.Person{AccountID: "b", Name: "Left", Baseline: 10, Active: false})

	active, _ := s.ListPeople(ctx(), false)
	if len(active) != 1 || active[0].AccountID != "a" {
		t.Errorf("active-only listing returned %+v", active)
	}
	all, _ := s.ListPeople(ctx(), true)
	if len(all) != 2 {
		t.Errorf("full listing should keep departed people, got %d", len(all))
	}
}

func testMissingPerson(t *testing.T, s store.Store) {
	if _, err := s.GetPerson(ctx(), "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// Jira's id is the key, because a sprint number is not unique across
// boards.
func testSprints(t *testing.T, s store.Store) {
	start := time.Now().UTC().Truncate(time.Second)
	if err := s.PutSprint(ctx(), store.Sprint{
		JiraID: 744, Label: "Sprint 21", Number: 21, State: "active", StartsAt: &start,
	}); err != nil {
		t.Fatalf("PutSprint: %v", err)
	}
	got, err := s.GetSprint(ctx(), 744)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if got.Number != 21 || got.Label != "Sprint 21" {
		t.Errorf("round-trip lost data: %+v", got)
	}
	if got.StartsAt == nil || !got.StartsAt.Equal(start) {
		t.Errorf("StartsAt lost: %v", got.StartsAt)
	}
	if got.ConfluencePageID != "" {
		t.Errorf("a sprint never published should have no page id, got %q", got.ConfluencePageID)
	}
	got.ConfluencePageID = "123"
	if err := s.PutSprint(ctx(), got); err != nil {
		t.Fatalf("PutSprint with a page id: %v", err)
	}
	if again, err := s.GetSprint(ctx(), 744); err != nil || again.ConfluencePageID != "123" {
		t.Errorf("the page id did not round-trip: %+v, err %v", again, err)
	}
}

// The sweep rewrites a sprint every time it rebuilds a report, and knows
// nothing about the page. A PutSprint without an id must therefore keep
// the stored one, or the second publish would create a second page.
func testPageIDSurvivesSweep(t *testing.T, s store.Store) {
	if err := s.PutSprint(ctx(), store.Sprint{JiraID: 744, Label: "Sprint 21", Number: 21, ConfluencePageID: "123"}); err != nil {
		t.Fatalf("PutSprint: %v", err)
	}
	if err := s.PutSprint(ctx(), store.Sprint{JiraID: 744, Label: "Sprint 21", Number: 21, State: "closed"}); err != nil {
		t.Fatalf("PutSprint from the sweep: %v", err)
	}
	got, err := s.GetSprint(ctx(), 744)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if got.ConfluencePageID != "123" || got.State != "closed" {
		t.Errorf("the re-sweep should update the sprint and keep its page id: %+v", got)
	}
	list, err := s.ListSprints(ctx(), 0)
	if err != nil || len(list) != 1 || list[0].ConfluencePageID != "123" {
		t.Errorf("ListSprints should carry the page id too: %+v, err %v", list, err)
	}
}

// Editing one sprint's capacity must not disturb another's.
func testCapacityIsolation(t *testing.T, s store.Store) {
	seed(t, s, 744, "a")
	seed(t, s, 745, "a")
	_ = s.PutCapacity(ctx(), store.Capacity{SprintJiraID: 744, AccountID: "a", PlannedDaysOff: 2, Reviewed: true})
	_ = s.PutCapacity(ctx(), store.Capacity{SprintJiraID: 745, AccountID: "a"})

	first, _ := s.ListCapacity(ctx(), 744)
	if len(first) != 1 || first[0].PlannedDaysOff != 2 || !first[0].Reviewed {
		t.Errorf("sprint 744 capacity wrong: %+v", first)
	}
	second, _ := s.ListCapacity(ctx(), 745)
	if len(second) != 1 || second[0].PlannedDaysOff != 0 || second[0].Reviewed {
		t.Errorf("sprint 745 capacity wrong: %+v", second)
	}
}

// The split is the whole point: planned absence belongs in the baseline,
// unplanned absence is what explains a delta nobody could have planned for.
func testAbsenceSplit(t *testing.T, s store.Store) {
	seed(t, s, 744, "a")
	if err := s.PutCapacity(ctx(), store.Capacity{
		SprintJiraID: 744, AccountID: "a", PlannedDaysOff: 3, UnplannedDaysOff: 1.5,
	}); err != nil {
		t.Fatalf("PutCapacity: %v", err)
	}
	rows, _ := s.ListCapacity(ctx(), 744)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].PlannedDaysOff != 3 || rows[0].UnplannedDaysOff != 1.5 {
		t.Errorf("split not preserved: %+v", rows[0])
	}
	if rows[0].DaysOff() != 4.5 {
		t.Errorf("DaysOff() = %v, want 4.5", rows[0].DaysOff())
	}
}

func testStats(t *testing.T, s store.Store) {
	seed(t, s, 744)
	if err := s.PutStats(ctx(), store.SprintStats{
		SprintJiraID: 744, BaselineTotal: 80, CapacityTotal: 72, DeliveredTotal: 68,
		Promised: 20, Injected: 4, Completed: 19,
		ByEpicClass: map[string]float64{"Run": 20, "Build": 48},
		StoriesDone: 3, StoryPointsDone: 21,
	}); err != nil {
		t.Fatalf("PutStats: %v", err)
	}
	all, err := s.ListStats(ctx(), 0)
	if err != nil {
		t.Fatalf("ListStats: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 stats row, got %d", len(all))
	}
	if all[0].ByEpicClass["Build"] != 48 {
		t.Errorf("epic class map lost: %+v", all[0].ByEpicClass)
	}
	if all[0].RecordedAt.IsZero() {
		t.Error("RecordedAt should be stamped")
	}
}

// The audit exists so a bulk edit can be explained afterwards, so it
// accumulates rather than replaces, newest first.
//
// The change set and outcome are what make a bulk edit readable as one
// action, and they have to survive both backends unchanged - including
// when absent, because every record written before they existed has
// neither. Filtering by change set is the caller's for now; the
// interface is not widened until something needs it.
func testWriteAudit(t *testing.T, s store.Store) {
	records := []store.WriteRecord{
		{Operation: "assign", Target: "ABC-123", Before: "x", After: "y", Actor: "me"},
		{Operation: "points.set", Target: "ABC-124", After: "3", Actor: "account-a",
			ChangeSet: "cs-1", Outcome: store.OutcomeApplied},
		{Operation: "points.set", Target: "ABC-125", After: "5", Actor: "account-a",
			ChangeSet: "cs-1", Outcome: store.OutcomeSkipped, Note: "guard: field no longer empty"},
	}
	for _, w := range records {
		if err := s.AppendWrite(ctx(), w); err != nil {
			t.Fatalf("AppendWrite: %v", err)
		}
	}
	all, err := s.ListWrites(ctx(), 0)
	if err != nil {
		t.Fatalf("ListWrites: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}
	if all[0].Target != "ABC-125" {
		t.Errorf("newest should be first, got %q", all[0].Target)
	}
	if all[0].At.IsZero() {
		t.Error("At should be stamped when not supplied")
	}

	// A skipped write is recorded alongside the applied one, under the
	// same change set: that is the whole record of a half-applied batch.
	if all[0].ChangeSet != "cs-1" || all[0].Outcome != store.OutcomeSkipped {
		t.Errorf("skipped write lost its context: %+v", all[0])
	}
	if all[1].ChangeSet != "cs-1" || all[1].Outcome != store.OutcomeApplied {
		t.Errorf("applied write lost its context: %+v", all[1])
	}

	// A record with neither reads back with neither, not with some
	// placeholder the other backend would not produce.
	if all[2].ChangeSet != "" || all[2].Outcome != "" {
		t.Errorf("a write outside any change set should read as empty, got %+v", all[2])
	}
}

// The draft is what lets a close-out be abandoned and resumed, so every
// field of every queued row has to come back exactly - including a
// points value of nil, which means "not typed" and is not zero.
func testDraft(t *testing.T, s store.Store) {
	seed(t, s, 744)
	if _, err := s.GetDraft(ctx(), 744); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a sprint nobody has queued against: want ErrNotFound, got %v", err)
	}

	three := 3.0
	started := time.Date(2026, 1, 16, 9, 0, 0, 0, time.UTC)
	if err := s.PutDraft(ctx(), store.Draft{SprintJiraID: 744, Requests: []store.DraftRequest{
		{Key: "ABC-1", Op: "points.set", Points: &three},
		{Key: "ABC-2", Op: "assignee.set", Assignee: "acc-b"},
		{Key: "ABC-3", Op: "worklog.add", Person: "acc-a", Hours: 4.5, Started: started, Note: "pairing"},
		{Key: "ABC-3", Op: "worklog.delete", WorklogID: "500"},
	}}); err != nil {
		t.Fatalf("PutDraft: %v", err)
	}

	got, err := s.GetDraft(ctx(), 744)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if got.SprintJiraID != 744 || len(got.Requests) != 4 {
		t.Fatalf("round-trip lost rows: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be stamped on write")
	}
	r := got.Requests
	if r[0].Key != "ABC-1" || r[0].Op != "points.set" || r[0].Points == nil || *r[0].Points != 3 {
		t.Errorf("points row did not survive: %+v", r[0])
	}
	if r[1].Points != nil || r[1].Assignee != "acc-b" {
		t.Errorf("assignee row did not survive: %+v", r[1])
	}
	if r[2].Person != "acc-a" || r[2].Hours != 4.5 || !r[2].Started.Equal(started) || r[2].Note != "pairing" {
		t.Errorf("worklog row did not survive: %+v", r[2])
	}
	if r[3].WorklogID != "500" || !r[3].Started.IsZero() {
		t.Errorf("delete row did not survive: %+v", r[3])
	}
}

// A save carries the whole queue, so the second save is the queue, not
// an addition to it - otherwise a row the person removed comes back.
func testDraftReplaced(t *testing.T, s store.Store) {
	seed(t, s, 744)
	_ = s.PutDraft(ctx(), store.Draft{SprintJiraID: 744, Requests: []store.DraftRequest{
		{Key: "ABC-1", Op: "assignee.set", Assignee: "acc-a"},
		{Key: "ABC-2", Op: "assignee.set", Assignee: "acc-a"},
	}})
	if err := s.PutDraft(ctx(), store.Draft{SprintJiraID: 744, Requests: []store.DraftRequest{
		{Key: "ABC-2", Op: "assignee.set", Assignee: "acc-b"},
	}}); err != nil {
		t.Fatalf("second PutDraft: %v", err)
	}
	got, err := s.GetDraft(ctx(), 744)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if len(got.Requests) != 1 || got.Requests[0].Assignee != "acc-b" {
		t.Errorf("the second save should replace the first, got %+v", got.Requests)
	}

	// An emptied queue is still a draft, and reads back as an empty list
	// rather than as nothing or as null.
	if err := s.PutDraft(ctx(), store.Draft{SprintJiraID: 744}); err != nil {
		t.Fatalf("saving an empty queue: %v", err)
	}
	got, err = s.GetDraft(ctx(), 744)
	if err != nil {
		t.Fatalf("GetDraft after emptying: %v", err)
	}
	if got.Requests == nil || len(got.Requests) != 0 {
		t.Errorf("an emptied queue should read as an empty list, got %#v", got.Requests)
	}
}

// Discard deletes the draft; discarding what is not there is not an
// error, because a double-click on Discard is not a mistake worth
// reporting.
func testDraftDiscarded(t *testing.T, s store.Store) {
	seed(t, s, 744)
	seed(t, s, 745)
	_ = s.PutDraft(ctx(), store.Draft{SprintJiraID: 744, Requests: []store.DraftRequest{{Key: "ABC-1", Op: "points.set"}}})
	_ = s.PutDraft(ctx(), store.Draft{SprintJiraID: 745, Requests: []store.DraftRequest{{Key: "ABC-2", Op: "points.set"}}})

	if err := s.DeleteDraft(ctx(), 744); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if _, err := s.GetDraft(ctx(), 744); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after discard: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteDraft(ctx(), 744); err != nil {
		t.Errorf("discarding twice should be quiet, got %v", err)
	}
	// One sprint's discard says nothing about another's.
	if other, err := s.GetDraft(ctx(), 745); err != nil || len(other.Requests) != 1 {
		t.Errorf("sprint 745's draft should survive, got %+v, err %v", other, err)
	}
}

// A draft for a sprint the store has never seen is a mistake, as capacity
// for one is: the panel only opens on a sprint whose report was built,
// and building it records the sprint.
func testDraftUnknownSprint(t *testing.T, s store.Store) {
	err := s.PutDraft(ctx(), store.Draft{SprintJiraID: 999, Requests: []store.DraftRequest{{Key: "ABC-1", Op: "points.set"}}})
	if !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("a draft for an unknown sprint: want ErrUnknownReference, got %v", err)
	}
	if _, err := s.GetDraft(ctx(), 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("reading a draft for an unknown sprint: want ErrNotFound, got %v", err)
	}
}

// First run is the normal case, not an error: an empty store reads as
// empty rather than failing.
func testEmptyStore(t *testing.T, s store.Store) {
	people, err := s.ListPeople(ctx(), true)
	if err != nil {
		t.Errorf("ListPeople: %v", err)
	}
	if len(people) != 0 {
		t.Errorf("expected no people, got %d", len(people))
	}
	if _, err := s.ListSprints(ctx(), 0); err != nil {
		t.Errorf("ListSprints: %v", err)
	}
	if _, err := s.ListCapacity(ctx(), 1); err != nil {
		t.Errorf("ListCapacity: %v", err)
	}
	if _, err := s.ListStats(ctx(), 0); err != nil {
		t.Errorf("ListStats: %v", err)
	}
	if _, err := s.ListWrites(ctx(), 0); err != nil {
		t.Errorf("ListWrites: %v", err)
	}
}

// Capacity for a sprint or person that does not exist is a mistake, not
// something to store quietly and puzzle over later.
func testUnknownReference(t *testing.T, s store.Store) {
	err := s.PutCapacity(ctx(), store.Capacity{SprintJiraID: 999, AccountID: "ghost"})
	if !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("capacity for an unknown sprint: want ErrUnknownReference, got %v", err)
	}

	seed(t, s, 744)
	err = s.PutCapacity(ctx(), store.Capacity{SprintJiraID: 744, AccountID: "ghost"})
	if !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("capacity for an unknown person: want ErrUnknownReference, got %v", err)
	}
}

// The three states a sprint's capacity can be in must be distinguishable.
// Rows and no review is a half-finished job; a review and no rows is the
// real answer "everybody was here"; neither is a sprint nobody has opened.
func testCapacityReview(t *testing.T, s store.Store) {
	seed(t, s, 744, "a")
	seed(t, s, 745, "a")

	at, err := s.CapacityReviewedAt(ctx(), 744)
	if err != nil {
		t.Fatalf("CapacityReviewedAt: %v", err)
	}
	if !at.IsZero() {
		t.Errorf("an untouched sprint should read as never reviewed, got %v", at)
	}

	before := time.Now().UTC().Add(-time.Second)
	if err := s.SetCapacityReviewed(ctx(), 744, true); err != nil {
		t.Fatalf("SetCapacityReviewed: %v", err)
	}
	at, err = s.CapacityReviewedAt(ctx(), 744)
	if err != nil {
		t.Fatalf("CapacityReviewedAt: %v", err)
	}
	if at.IsZero() || at.Before(before) {
		t.Errorf("review should be stamped with the time it happened, got %v", at)
	}

	// "Everybody was available" means exactly no capacity rows, so the
	// review must not invent any.
	if rows, _ := s.ListCapacity(ctx(), 744); len(rows) != 0 {
		t.Errorf("reviewing should write no capacity rows, got %d", len(rows))
	}

	// One sprint's review says nothing about another's.
	if other, _ := s.CapacityReviewedAt(ctx(), 745); !other.IsZero() {
		t.Errorf("sprint 745 should still be unreviewed, got %v", other)
	}

	// A review can be reopened, which must put the sprint back to
	// unreviewed rather than to some third state.
	if err := s.SetCapacityReviewed(ctx(), 744, false); err != nil {
		t.Fatalf("withdrawing the review: %v", err)
	}
	if at, _ := s.CapacityReviewedAt(ctx(), 744); !at.IsZero() {
		t.Errorf("a withdrawn review should read as never reviewed, got %v", at)
	}

	if err := s.SetCapacityReviewed(ctx(), 999, true); !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("reviewing an unknown sprint: want ErrUnknownReference, got %v", err)
	}
	if _, err := s.CapacityReviewedAt(ctx(), 999); !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("reading an unknown sprint's review: want ErrUnknownReference, got %v", err)
	}
}

// The sweep rewrites a sprint every time it rebuilds a report. If that
// cleared the review, a sprint confirmed as needing no adjustments would
// quietly go back to looking untouched a few minutes later.
func testReviewSurvivesSweep(t *testing.T, s store.Store) {
	seed(t, s, 744, "a")
	if err := s.SetCapacityReviewed(ctx(), 744, true); err != nil {
		t.Fatalf("SetCapacityReviewed: %v", err)
	}

	ends := time.Now().UTC()
	if err := s.PutSprint(ctx(), store.Sprint{
		JiraID: 744, Label: "Sprint X", Number: 744, State: "closed", EndsAt: &ends,
	}); err != nil {
		t.Fatalf("re-sweeping the sprint: %v", err)
	}

	if at, _ := s.CapacityReviewedAt(ctx(), 744); at.IsZero() {
		t.Error("a re-sweep must not clear the capacity review")
	}
}

// Retention is what keeps the tool from becoming a permanent archive of
// who was away when. It must drop the per-person rows and keep the
// aggregates the trends are drawn from.
func testRetention(t *testing.T, s store.Store) {
	old := time.Now().UTC().Add(-4 * 365 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-24 * time.Hour)

	if err := s.PutSprint(ctx(), store.Sprint{JiraID: 1, Label: "old", Number: 1, EndsAt: &old}); err != nil {
		t.Fatalf("PutSprint: %v", err)
	}
	if err := s.PutSprint(ctx(), store.Sprint{JiraID: 2, Label: "recent", Number: 2, EndsAt: &recent}); err != nil {
		t.Fatalf("PutSprint: %v", err)
	}
	_ = s.PutPerson(ctx(), store.Person{AccountID: "a", Name: "A", Baseline: 10, Active: true})
	_ = s.PutCapacity(ctx(), store.Capacity{SprintJiraID: 1, AccountID: "a", PlannedDaysOff: 2, Note: "private"})
	_ = s.PutCapacity(ctx(), store.Capacity{SprintJiraID: 2, AccountID: "a", PlannedDaysOff: 1})
	_ = s.PutStats(ctx(), store.SprintStats{SprintJiraID: 1, DeliveredTotal: 50})
	_ = s.PutStats(ctx(), store.SprintStats{SprintJiraID: 2, DeliveredTotal: 60})
	_ = s.PutDraft(ctx(), store.Draft{SprintJiraID: 1, Requests: []store.DraftRequest{{Key: "ABC-1", Op: "assignee.set", Assignee: "a"}}})
	_ = s.PutDraft(ctx(), store.Draft{SprintJiraID: 2, Requests: []store.DraftRequest{{Key: "ABC-2", Op: "assignee.set", Assignee: "a"}}})

	cutoff := time.Now().UTC().Add(-3 * 365 * 24 * time.Hour)
	res, err := s.Prune(ctx(), cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.CapacityRows != 1 {
		t.Errorf("expected 1 capacity row pruned, got %d", res.CapacityRows)
	}
	if res.Drafts != 1 {
		t.Errorf("expected 1 draft pruned, got %d", res.Drafts)
	}

	if rows, _ := s.ListCapacity(ctx(), 1); len(rows) != 0 {
		t.Errorf("old sprint should have no capacity rows left, got %d", len(rows))
	}
	if rows, _ := s.ListCapacity(ctx(), 2); len(rows) != 1 {
		t.Errorf("recent sprint capacity must survive, got %d rows", len(rows))
	}
	// A draft names tickets and people for a close nobody will now run.
	if _, err := s.GetDraft(ctx(), 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("old sprint's draft should be gone, got %v", err)
	}
	if d, err := s.GetDraft(ctx(), 2); err != nil || len(d.Requests) != 1 {
		t.Errorf("recent sprint's draft must survive, got %+v, err %v", d, err)
	}

	// The whole point: aggregates outlive the personal data.
	stats, _ := s.ListStats(ctx(), 0)
	if len(stats) != 2 {
		t.Errorf("stats carry no personal data and must survive pruning, got %d", len(stats))
	}
}

// Both backends version their storage, and both apply that version on
// startup: the file backend stamps a marker in the data directory, and
// Postgres runs its migrations. Argus calls Migrate on every start, so on
// both it has to be safe to call on an empty store, safe to call again,
// and incapable of disturbing what is already there.
//
// This is asserted here rather than in each backend's own tests because
// it is the behaviour the application depends on, and a backend free to
// differ on it would break the one operation nobody watches.
func testMigrateIsRepeatable(t *testing.T, s store.Store) {
	if err := s.Migrate(ctx()); err != nil {
		t.Fatalf("Migrate on an empty store: %v", err)
	}
	if err := s.Migrate(ctx()); err != nil {
		t.Fatalf("Migrate a second time: %v", err)
	}

	seed(t, s, 744, "a")
	if err := s.PutCapacity(ctx(), store.Capacity{
		SprintJiraID: 744, AccountID: "a", PlannedDaysOff: 2, UnplannedDaysOff: 1, Reviewed: true,
	}); err != nil {
		t.Fatalf("PutCapacity: %v", err)
	}
	if err := s.SetCapacityReviewed(ctx(), 744, true); err != nil {
		t.Fatalf("SetCapacityReviewed: %v", err)
	}

	// Startup migration must be a no-op over existing data, not a rewrite
	// of it.
	if err := s.Migrate(ctx()); err != nil {
		t.Fatalf("Migrate over existing data: %v", err)
	}

	people, err := s.ListPeople(ctx(), true)
	if err != nil || len(people) != 1 || people[0].AccountID != "a" {
		t.Errorf("the roster did not survive Migrate: %+v, err %v", people, err)
	}
	rows, err := s.ListCapacity(ctx(), 744)
	if err != nil || len(rows) != 1 {
		t.Fatalf("capacity did not survive Migrate: %+v, err %v", rows, err)
	}
	if rows[0].PlannedDaysOff != 2 || rows[0].UnplannedDaysOff != 1 || !rows[0].Reviewed {
		t.Errorf("capacity changed across Migrate: %+v", rows[0])
	}
	if at, _ := s.CapacityReviewedAt(ctx(), 744); at.IsZero() {
		t.Error("the capacity review did not survive Migrate")
	}
}
