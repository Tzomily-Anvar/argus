package sprint

import (
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// After an apply the draft keeps only what did not land: the rows that
// were applied are gone, the skipped ones stay to be tried again, and an
// emptied draft is deleted rather than stored empty.
func TestClearAppliedDropsTheRowsThatLanded(t *testing.T) {
	svc, sp, st := cachedService(t)
	ctx := t.Context()
	three := 3.0
	if err := st.PutDraft(ctx, store.Draft{SprintJiraID: sp.ID, Requests: []store.DraftRequest{
		{Key: "ABC-1", Op: OpPointsSet, Points: &three},
		{Key: "ABC-2", Op: OpWorklogAdd, Person: "acc-a", Hours: 2},
		{Key: "ABC-2", Op: OpWorklogAdd, Person: "acc-b", Hours: 1},
		{Key: "ABC-3", Op: OpWorklogDelete, WorklogID: "45"},
	}}); err != nil {
		t.Fatal(err)
	}
	changes := []Change{
		{Key: "ABC-1", Op: OpPointsSet},
		{Key: "ABC-2", Op: OpWorklogAdd, Person: "acc-a"},
		{Key: "ABC-2", Op: OpWorklogAdd, Person: "acc-b"},
		{Key: "ABC-3", Op: OpWorklogDelete, WorklogID: "45"},
	}
	rows := []RowResult{
		{Outcome: store.OutcomeApplied},
		{Outcome: store.OutcomeApplied},
		{Outcome: store.OutcomeSkipped},
		{Outcome: store.OutcomeFailed},
	}
	svc.clearApplied(ctx, sp.ID, changes, rows)
	d, err := st.GetDraft(ctx, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Requests) != 2 || d.Requests[0].Person != "acc-b" || d.Requests[1].WorklogID != "45" {
		t.Fatalf("draft after apply = %+v; want the skipped and failed rows only", d.Requests)
	}

	svc.clearApplied(ctx, sp.ID, changes[2:], []RowResult{{Outcome: store.OutcomeApplied}, {Outcome: store.OutcomeApplied}})
	if _, err := st.GetDraft(ctx, sp.ID); err == nil {
		t.Error("a draft with nothing left should be deleted, not stored empty")
	}
}
