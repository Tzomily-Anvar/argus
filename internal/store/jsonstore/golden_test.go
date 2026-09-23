package jsonstore_test

// A data directory as Argus writes it today, kept and re-read on every
// test run.
//
// The registry test in internal/store says when the shape of a struct
// changed. This says whether data already on disk still reads, which is
// the question that actually matters: a rename passes every unit test in
// the codebase, because the new field simply reads as its zero value and
// nothing is there to notice. The only way to catch it is to keep a
// directory written by an older Argus and insist it still loads with the
// right values in it.
//
// The fixture is built from the code and the conformance suite, never
// copied from a real installation: what the file backend holds is
// colleagues' names, Jira account ids and how much each of them was away.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

const goldenDir = "testdata/golden/v1"

// goldenFiles is every file the fixture holds. A file renamed, split or
// merged is as destructive as a renamed field and just as quiet, so the
// set is asserted rather than assumed.
var goldenFiles = []string{
	"capacity-744.json",
	"capacity-reviews.json",
	"people.json",
	"sprints.json",
	"stats.json",
	"writes.json",
}

// copyGolden puts the fixture somewhere writable, so a test that migrates
// or prunes cannot edit the checked-in copy.
func copyGolden(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var names []string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(goldenDir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o640); err != nil {
			t.Fatalf("writing %s: %v", e.Name(), err)
		}
		names = append(names, e.Name())
	}
	if len(names) != len(goldenFiles) {
		t.Fatalf("fixture holds %v, expected %v: if a file was renamed, split or "+
			"merged, every existing install still has the old one and nothing "+
			"reads it. Keep reading the old name for a release.", names, goldenFiles)
	}
	for i, want := range goldenFiles {
		if names[i] != want {
			t.Fatalf("fixture file %d is %q, expected %q", i, names[i], want)
		}
	}
	return dir
}

func ctx() context.Context { return context.Background() }

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return ts
}

// TestGoldenDirectoryStillLoads is the regression test. Every field is
// checked against the literal value in the fixture, because a field that
// merely decodes without error has told you nothing: a renamed field
// decodes perfectly and arrives empty.
func TestGoldenDirectoryStillLoads(t *testing.T) {
	dir := copyGolden(t)
	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("a directory written by an earlier Argus must still open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	checkGolden(t, s)
}

// TestGoldenDirectoryMigrates covers the upgrade path: an install that
// predates versioning gets stamped and keeps every value it had.
func TestGoldenDirectoryMigrates(t *testing.T) {
	dir := copyGolden(t)
	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(ctx()); err != nil {
		t.Fatalf("Migrate on an unversioned directory: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "schema.json"))
	if err != nil {
		t.Fatalf("Migrate should record the schema version: %v", err)
	}
	var marker struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(b, &marker); err != nil {
		t.Fatalf("parsing schema.json: %v", err)
	}
	if marker.SchemaVersion != jsonstore.SchemaVersion {
		t.Errorf("schema.json records version %d, want %d", marker.SchemaVersion, jsonstore.SchemaVersion)
	}

	// Migrating must not have disturbed anything it found.
	checkGolden(t, s)
}

// TestGoldenFilesHaveNoUnknownFields decodes the fixture strictly. A
// field renamed in the struct turns the fixture's old name into an
// unknown one, which is the same thing that happens to every install's
// real data - except here it fails the build instead of reading as zero.
func TestGoldenFilesHaveNoUnknownFields(t *testing.T) {
	cases := []struct {
		file string
		into any
	}{
		{"people.json", &[]store.Person{}},
		{"sprints.json", &[]store.Sprint{}},
		{"capacity-744.json", &[]store.Capacity{}},
		{"stats.json", &[]store.SprintStats{}},
		{"writes.json", &[]store.WriteRecord{}},
		{"capacity-reviews.json", &map[int64]time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(goldenDir, c.file))
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.DisallowUnknownFields()
			if err := dec.Decode(c.into); err != nil {
				t.Fatalf("%s no longer decodes into the current structs: %v\n\n"+
					"This file is a copy of what every existing install has on disk. "+
					"A field named here that the struct no longer knows - because it "+
					"was renamed or removed - reads as its zero value in real data, "+
					"and nothing anywhere notices. Keep reading the old name for a "+
					"release, then update this fixture.", c.file, err)
			}
		})
	}
}

// checkGolden asserts every stored value, field by field.
func checkGolden(t *testing.T, s store.Store) {
	t.Helper()

	people, err := s.ListPeople(ctx(), true)
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("expected 2 people, got %d", len(people))
	}
	a := people[0]
	if a.AccountID != "acc-a" || a.Name != "Person A" || a.Baseline != 12.5 || !a.Active {
		t.Errorf("person A did not survive: %+v", a)
	}
	if !a.UpdatedAt.Equal(mustTime(t, "2026-01-05T09:00:00Z")) {
		t.Errorf("person A updated_at = %v", a.UpdatedAt)
	}
	b := people[1]
	if b.AccountID != "acc-b" || b.Name != "Person B" || b.Baseline != 8 || b.Active {
		t.Errorf("person B did not survive: %+v", b)
	}

	// Someone who has left must still be filterable out, not lost.
	active, err := s.ListPeople(ctx(), false)
	if err != nil {
		t.Fatalf("ListPeople(active): %v", err)
	}
	if len(active) != 1 || active[0].AccountID != "acc-a" {
		t.Errorf("active-only listing returned %+v", active)
	}

	sp, err := s.GetSprint(ctx(), 744)
	if err != nil {
		t.Fatalf("GetSprint(744): %v", err)
	}
	if sp.Label != "Sprint 21" || sp.Number != 21 || sp.State != "closed" {
		t.Errorf("sprint 744 did not survive: %+v", sp)
	}
	if sp.StartsAt == nil || !sp.StartsAt.Equal(mustTime(t, "2026-01-05T00:00:00Z")) {
		t.Errorf("sprint 744 starts_at = %v", sp.StartsAt)
	}
	if sp.EndsAt == nil || !sp.EndsAt.Equal(mustTime(t, "2026-01-16T17:00:00Z")) {
		t.Errorf("sprint 744 ends_at = %v", sp.EndsAt)
	}
	if sprints, err := s.ListSprints(ctx(), 0); err != nil || len(sprints) != 2 {
		t.Errorf("ListSprints returned %d sprints, err %v", len(sprints), err)
	}

	rows, err := s.ListCapacity(ctx(), 744)
	if err != nil {
		t.Fatalf("ListCapacity(744): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 capacity rows, got %d", len(rows))
	}
	// The planned/unplanned split is the field most easily lost to a
	// rename, and the one that carries the entire explanation of a delta.
	if rows[0].AccountID != "acc-a" || rows[0].PlannedDaysOff != 2 || rows[0].UnplannedDaysOff != 1.5 {
		t.Errorf("capacity for acc-a did not survive: %+v", rows[0])
	}
	if rows[0].DaysOff() != 3.5 {
		t.Errorf("DaysOff() = %v, want 3.5", rows[0].DaysOff())
	}
	if !rows[0].Reviewed {
		t.Error("acc-a's capacity was confirmed by a human; reviewed must not read as false")
	}
	if rows[0].Note == "" {
		t.Error("the note explaining acc-a's delta was lost")
	}
	// An omitted note is a real absence of one, not a lost field.
	if rows[1].AccountID != "acc-b" || rows[1].Note != "" || !rows[1].Reviewed {
		t.Errorf("capacity for acc-b did not survive: %+v", rows[1])
	}

	// A sprint reviewed with no adjustments needed is a different fact
	// from a sprint nobody opened, and that distinction lives in its own
	// file.
	at, err := s.CapacityReviewedAt(ctx(), 744)
	if err != nil {
		t.Fatalf("CapacityReviewedAt(744): %v", err)
	}
	if !at.Equal(mustTime(t, "2026-01-17T09:20:00Z")) {
		t.Errorf("capacity review for 744 = %v, want the fixture's timestamp", at)
	}
	if at, err := s.CapacityReviewedAt(ctx(), 745); err != nil || !at.IsZero() {
		t.Errorf("sprint 745 was never reviewed; got %v, err %v", at, err)
	}

	stats, err := s.ListStats(ctx(), 0)
	if err != nil {
		t.Fatalf("ListStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stats row, got %d", len(stats))
	}
	st := stats[0]
	if st.SprintJiraID != 744 || st.BaselineTotal != 20.5 || st.CapacityTotal != 17 || st.DeliveredTotal != 16 {
		t.Errorf("stats totals did not survive: %+v", st)
	}
	if st.PlannedDaysOff != 2 || st.UnplannedDaysOff != 1.5 {
		t.Errorf("stats absence split did not survive: %+v", st)
	}
	if st.Promised != 9 || st.Injected != 3 || st.Completed != 8 {
		t.Errorf("stats counts did not survive: %+v", st)
	}
	if st.StoriesDone != 4 || st.StoryPointsDone != 13 {
		t.Errorf("stats story figures did not survive: %+v", st)
	}
	if st.ByEpicClass["Build"] != 11 || st.ByEpicClass["Run"] != 5 {
		t.Errorf("epic class breakdown did not survive: %+v", st.ByEpicClass)
	}
	if !st.RecordedAt.Equal(mustTime(t, "2026-01-17T10:00:00Z")) {
		t.Errorf("stats recorded_at = %v", st.RecordedAt)
	}

	writes, err := s.ListWrites(ctx(), 0)
	if err != nil {
		t.Fatalf("ListWrites: %v", err)
	}
	if len(writes) != 2 {
		t.Fatalf("expected 2 write records, got %d", len(writes))
	}
	w := writes[0]
	if w.ID != 2 || w.Operation != "set_points" || w.Target != "ABC-123" {
		t.Errorf("newest write record did not survive: %+v", w)
	}
	if w.Before != "3" || w.After != "5" || w.Actor != "argus" || w.Note == "" {
		t.Errorf("write record detail did not survive: %+v", w)
	}
	if !w.At.Equal(mustTime(t, "2026-01-16T14:05:00Z")) {
		t.Errorf("write record at = %v", w.At)
	}
	if writes[1].Operation != "assign" || writes[1].After != "acc-a" {
		t.Errorf("older write record did not survive: %+v", writes[1])
	}

	// The close-out draft arrived after this fixture was written, so no
	// install has a draft file. A directory without one has to read as
	// "nothing queued", not as an error.
	if _, err := s.GetDraft(ctx(), 744); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a directory with no draft file should read as ErrNotFound, got %v", err)
	}
}
