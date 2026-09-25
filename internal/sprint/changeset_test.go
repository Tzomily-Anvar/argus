package sprint_test

import (
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The digest is what lets "what was applied is what was previewed" be
// checked rather than asserted. It has to move when anything that will be
// written moves, and stay put when only the wording does.

func twoChanges() []sprint.Change {
	at := time.Date(2026, 1, 19, 10, 0, 0, 0, time.UTC)
	return []sprint.Change{
		{
			Key: "ABC-1", Op: sprint.OpPointsSet, Field: "customfield_10016", After: 3.0, AfterLabel: "3",
			Reason: "finished with no estimate",
			Guard:  sprint.Guard{IssueID: "10001", Updated: at, Status: "Done"},
		},
		{
			Key: "ABC-2", Op: sprint.OpAssigneeSet, Field: "assignee",
			After: map[string]string{"accountId": "acc-a"}, AfterLabel: "Person A",
			Reason: "finished unassigned",
			Guard:  sprint.Guard{IssueID: "10002", Updated: at, Status: "Done"},
		},
	}
}

func TestDigestIsStableForIdenticalInput(t *testing.T) {
	a, b := sprint.Digest(twoChanges()), sprint.Digest(twoChanges())
	if a == "" || a != b {
		t.Errorf("identical changes digested to %q and %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("digest %q is not a hex sha256", a)
	}
}

func TestDigestChangesWhenOrderChanges(t *testing.T) {
	forward := twoChanges()
	backward := []sprint.Change{forward[1], forward[0]}
	if sprint.Digest(forward) == sprint.Digest(backward) {
		t.Error("reordering the changes left the digest unchanged; a row shown in the wrong place would pass")
	}
}

func TestDigestChangesWhenAGuardMoves(t *testing.T) {
	moved := twoChanges()
	moved[0].Guard.Updated = moved[0].Guard.Updated.Add(time.Minute)
	if sprint.Digest(twoChanges()) == sprint.Digest(moved) {
		t.Error("a guard that moved left the digest unchanged")
	}

	edited := twoChanges()
	edited[0].After = 5.0
	if sprint.Digest(twoChanges()) == sprint.Digest(edited) {
		t.Error("a different value left the digest unchanged")
	}
}

// Labels and reasons are for reading. Rewording one must not invalidate
// a preview somebody is looking at.
func TestDigestIgnoresTheWording(t *testing.T) {
	reworded := twoChanges()
	reworded[0].AfterLabel = "three"
	reworded[0].Reason = "typed by hand"
	reworded[1].Summary = "another summary"
	if sprint.Digest(twoChanges()) != sprint.Digest(reworded) {
		t.Error("changing only the labels changed the digest")
	}
}

func TestDigestOfNothingIsStillADigest(t *testing.T) {
	if d := sprint.Digest(nil); d == "" || d != sprint.Digest([]sprint.Change{}) {
		t.Errorf("an empty change list should digest to one fixed value, got %q", d)
	}
}
