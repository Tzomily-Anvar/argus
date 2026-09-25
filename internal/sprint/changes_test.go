package sprint

// In the package itself, like service_test.go, because the holder and the
// cache are deliberately not part of the exported surface.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

func TestHeldChangeSetsExpire(t *testing.T) {
	now := time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC)
	h := newChangeSets(func() time.Time { return now })

	cs := h.Put(ChangeSet{Digest: "abc", ExpiresAt: now.Add(ChangeSetLifetime)})
	if cs.ID == "" || len(cs.ID) != 32 {
		t.Fatalf("Put should assign a random id, got %q", cs.ID)
	}
	if got := h.Get(cs.ID); got == nil || got.Digest != "abc" {
		t.Fatalf("Get right after Put = %v", got)
	}
	if h.Get("nothing-here") != nil {
		t.Error("an unknown id must come back nil")
	}

	now = now.Add(ChangeSetLifetime - time.Second)
	if h.Get(cs.ID) == nil {
		t.Error("a second before expiry the set should still be there")
	}
	now = now.Add(time.Second)
	if h.Get(cs.ID) != nil {
		t.Error("at expiry the set must be gone")
	}
}

func TestHeldChangeSetIsTakenOnce(t *testing.T) {
	now := time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC)
	h := newChangeSets(func() time.Time { return now })
	cs := h.Put(ChangeSet{Digest: "abc", ExpiresAt: now.Add(ChangeSetLifetime)})

	if got := h.Take(cs.ID); got == nil || got.ID != cs.ID {
		t.Fatalf("first Take = %v", got)
	}
	if h.Take(cs.ID) != nil {
		t.Error("a second Take must get nothing; that is what stops a double-click writing twice")
	}
	if h.Get(cs.ID) != nil {
		t.Error("a taken set must not be readable either")
	}
}

// Two ids from the same holder are never the same, and a caller changing
// what it was handed does not change what is held.
func TestHeldChangeSetsAreCopies(t *testing.T) {
	now := time.Now()
	h := newChangeSets(func() time.Time { return now })
	a := h.Put(ChangeSet{ExpiresAt: now.Add(time.Hour)})
	b := h.Put(ChangeSet{ExpiresAt: now.Add(time.Hour)})
	if a.ID == b.ID {
		t.Fatal("two sets share an id")
	}
	got := h.Get(a.ID)
	got.Digest = "tampered"
	if h.Get(a.ID).Digest == "tampered" {
		t.Error("Get handed out the held value itself rather than a copy")
	}
}

func TestServiceProposeNeedsAReportFirst(t *testing.T) {
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := NewService(nil, st, Config{})

	_, err = svc.Propose(context.Background(), 21, []ChangeRequest{{Key: "ABC-1", Op: OpPointsSet}})
	if !errors.Is(err, ErrNoReport) {
		t.Fatalf("err = %v, want ErrNoReport", err)
	}
	if !strings.Contains(err.Error(), "21") {
		t.Errorf("the error should name the sprint: %q", err)
	}
}

// With a report in the cache a proposal needs nothing from Jira: the
// client here is nil and would panic if reached. The one issue is done,
// assigned and sized, so every request is refused for a reason that
// proves the cached report and its field ids were what was consulted.
func TestServiceProposeUsesTheCachedReport(t *testing.T) {
	svc, _, _ := cachedService(t)
	three := 3.0

	cs, err := svc.Propose(context.Background(), 21, []ChangeRequest{
		{Key: "ABC-1", Op: OpPointsSet, Points: &three},
		{Key: "ABC-1", Op: OpAssigneeSet, Assignee: "acc-a"},
		{Key: "ABC-7", Op: OpPointsSet, Points: &three},
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if cs.ID == "" || cs.SprintJiraID != 744 || len(cs.Changes) != 0 || len(cs.Skipped) != 3 {
		t.Fatalf("change set = %+v", cs)
	}
	for i, want := range []string{"already has 5 points", "already assigned", "not in this sprint's report"} {
		if !strings.Contains(cs.Skipped[i].Reason, want) {
			t.Errorf("skip %d = %q, want it to mention %q", i, cs.Skipped[i].Reason, want)
		}
	}

	if got := svc.ChangeSet(cs.ID); got == nil || got.Digest != cs.Digest {
		t.Fatalf("the proposal was not held: %v", got)
	}
	if svc.TakeChangeSet(cs.ID) == nil || svc.ChangeSet(cs.ID) != nil {
		t.Error("TakeChangeSet must hand the set out once and remove it")
	}

	if _, err := svc.Propose(context.Background(), 22, nil); !errors.Is(err, ErrNoReport) {
		t.Errorf("a sprint not in the cache: err = %v, want ErrNoReport", err)
	}
}
