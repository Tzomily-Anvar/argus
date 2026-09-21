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
		"stats round-trip":                 testStats,
		"writes are append-only":           testWriteAudit,
		"empty store reads as empty":       testEmptyStore,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, fresh(t)) })
	}
}

func ctx() context.Context { return context.Background() }

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
