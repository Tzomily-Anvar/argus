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
		"capacity is isolated per sprint":  testCapacityIsolation,
		"absence splits planned/unplanned": testAbsenceSplit,
		"capacity needs known references":  testUnknownReference,
		"a sprint can be reviewed":         testCapacityReview,
		"a review survives a re-sweep":     testReviewSurvivesSweep,
		"retention keeps aggregates":       testRetention,
		"stats round-trip":                 testStats,
		"writes are append-only":           testWriteAudit,
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
func testWriteAudit(t *testing.T, s store.Store) {
	for _, op := range []string{"assign", "set_points"} {
		if err := s.AppendWrite(ctx(), store.WriteRecord{
			Operation: op, Target: "PROJ-1", Before: "x", After: "y", Actor: "me",
		}); err != nil {
			t.Fatalf("AppendWrite: %v", err)
		}
	}
	all, err := s.ListWrites(ctx(), 0)
	if err != nil {
		t.Fatalf("ListWrites: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	if all[0].Operation != "set_points" {
		t.Errorf("newest should be first, got %q", all[0].Operation)
	}
	if all[0].At.IsZero() {
		t.Error("At should be stamped when not supplied")
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

	cutoff := time.Now().UTC().Add(-3 * 365 * 24 * time.Hour)
	res, err := s.Prune(ctx(), cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.CapacityRows != 1 {
		t.Errorf("expected 1 capacity row pruned, got %d", res.CapacityRows)
	}

	if rows, _ := s.ListCapacity(ctx(), 1); len(rows) != 0 {
		t.Errorf("old sprint should have no capacity rows left, got %d", len(rows))
	}
	if rows, _ := s.ListCapacity(ctx(), 2); len(rows) != 1 {
		t.Errorf("recent sprint capacity must survive, got %d rows", len(rows))
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
