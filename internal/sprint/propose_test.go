package sprint_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// wired builds an issue the way Jira would send it, with an id and an
// updated time for the guard. An empty assignee is no assignee, and a
// nil points is an empty field - which the issue helper in build_test
// cannot express, since it always writes the field.
func wired(t *testing.T, key, id, issueType, status, assignee string, points *float64) jira.Issue {
	t.Helper()
	fields := map[string]any{
		"summary":   "Something about " + key,
		"issuetype": map[string]any{"name": issueType},
		"status":    map[string]any{"name": status},
		"created":   "2026-01-01T09:00:00.000+0000",
		"updated":   "2026-01-18T09:00:00.000+0000",
	}
	if assignee != "" {
		fields["assignee"] = map[string]any{"accountId": assignee, "displayName": "Person " + assignee}
	}
	if points != nil {
		fields[pointsField] = *points
	}
	if status == "Done" {
		fields["resolutiondate"] = "2026-01-10T09:00:00.000+0000"
	}
	b, err := json.Marshal(map[string]any{"id": id, "key": key, "fields": fields})
	if err != nil {
		t.Fatalf("encoding the test issue: %v", err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatalf("decoding the test issue: %v", err)
	}
	return is
}

// proposal builds one report and proposes against it. The sprint under
// test is the one from inputs: opens 5 January, ends 19 January, and the
// end is the close because it has no completion date.
func proposal(t *testing.T, reqs ...sprint.ChangeRequest) sprint.ChangeSet {
	t.Helper()
	five, eight := 5.0, 8.0
	carried := wired(t, "ABC-2", "10002", "Task", "In Progress", "acc-a", nil)
	carried.Fields.Worklog.Entries = []jira.WorklogEntry{{
		ID:      "500",
		Started: jira.Time{Time: time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)},
		Updated: jira.Time{Time: time.Date(2026, 1, 12, 10, 5, 0, 0, time.UTC)},
		Seconds: 3 * 3600,
		Author:  &jira.User{AccountID: "acc-lead"},
		Comment: json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"acc-a","text":"@Person A"}}]}]}`),
	}}
	carried.Fields.Worklog.Total = 1
	carried.Fields.TimeSpent = 3 * 3600

	issues := []jira.Issue{
		wired(t, "ABC-1", "10001", "Task", "Done", "", nil), // finished, unassigned, unsized
		carried, // open at close, time logged
		wired(t, "ABC-3", "10003", "Task", "To Do", "", nil),       // open at close, nothing logged
		wired(t, "ABC-4", "10004", "Story", "Done", "", nil),       // a container, unsized
		wired(t, "ABC-5", "10005", "Task", "Done", "acc-a", &five), // finished, complete
		wired(t, "ABC-6", "10006", "Story", "Done", "", &eight),    // a container with a figure already
	}
	in := inputs(t)
	in.Issues = issues
	in.People = []store.Person{
		{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true},
		{AccountID: "acc-b", Name: "Person B", Baseline: 10, Active: true},
		{AccountID: "acc-gone", Name: "Person Gone", Baseline: 10, Active: false},
	}
	rep := sprint.Build(in)
	opens, closes := in.Sprint.StartDate.Time, in.Sprint.EndDate.Time

	return sprint.Propose(rep, issues, sprint.ProposalInputs{
		Requests: reqs, People: in.People, Rules: in.Rules, PointsField: pointsField,
		HoursPerPoint: 6, SprintOpens: opens, SprintCloses: closes,
		Now: time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC), Actor: "operator@example.com",
	})
}

func TestProposeRules(t *testing.T) {
	cases := []struct {
		name   string
		req    sprint.ChangeRequest
		skip   string // a fragment of the reason; empty means a change is expected
		reason string // the change's reason, when one is expected
	}{
		{"points on a finished unsized task", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpPointsSet, Points: num(3)}, "", "finished with no estimate"},
		{"points on an open unsized task", sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpPointsSet, Points: num(2)}, "", "open with no estimate"},
		{"points where there already are some", sprint.ChangeRequest{Key: "ABC-5", Op: sprint.OpPointsSet, Points: num(3)}, "already has 5 points", ""},
		{"points on a container with none", sprint.ChangeRequest{Key: "ABC-4", Op: sprint.OpPointsSet, Points: num(3)}, "", "story wrapped up; points set to the sum of its work"},
		{"points on a container that has some", sprint.ChangeRequest{Key: "ABC-6", Op: sprint.OpPointsSet, Points: num(3)}, "", "story wrapped up; points corrected to the sum of its work"},
		{"points of zero on a container", sprint.ChangeRequest{Key: "ABC-6", Op: sprint.OpPointsSet, Points: num(0)}, "above zero", ""},
		{"points a container already holds", sprint.ChangeRequest{Key: "ABC-6", Op: sprint.OpPointsSet, Points: num(8)}, "already holds 8 points", ""},
		{"an entry that is already there", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 3, Started: time.Date(2026, 1, 12, 15, 0, 0, 0, time.UTC)}, "already has 3h logged", ""},
		{"the same hours on another day are new", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 3}, "", "carried over, more time logged"},
		{"points of zero", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpPointsSet, Points: num(0)}, "above zero", ""},
		{"points left untyped", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpPointsSet}, "above zero", ""},
		{"a key the report does not know", sprint.ChangeRequest{Key: "ABC-9", Op: sprint.OpPointsSet, Points: num(3)}, "not in this sprint's report", ""},
		{"an operation nobody agreed to", sprint.ChangeRequest{Key: "ABC-1", Op: "status.set"}, "not an operation", ""},

		{"assignee on a finished unassigned task", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpAssigneeSet, Assignee: "acc-b"}, "", "finished unassigned"},
		{"assignee where there already is one", sprint.ChangeRequest{Key: "ABC-5", Op: sprint.OpAssigneeSet, Assignee: "acc-b"}, "already assigned", ""},
		{"assignee off the roster", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpAssigneeSet, Assignee: "acc-x"}, "not on the roster", ""},
		{"assignee who has left", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpAssigneeSet, Assignee: "acc-gone"}, "having left", ""},
		{"no assignee chosen", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpAssigneeSet}, "no assignee was chosen", ""},

		{"time on carryover nobody logged against", sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-b", Hours: 6}, "", "carried with no time logged"},
		{"more time on carryover", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogAdd, Person: "acc-b", Hours: 1.5}, "", "carried over, more time logged"},
		{"time on a finished task", sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 6}, "not open when the sprint closed", ""},
		{"time for somebody off the roster", sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-x", Hours: 6}, "not on the roster", ""},
		{"no hours", sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-a"}, "hours must be above zero", ""},
		{"dated after the sprint", sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 6, Started: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)}, "outside the sprint window", ""},
		{"dated before the sprint", sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 6, Started: time.Date(2025, 12, 1, 9, 0, 0, 0, time.UTC)}, "outside the sprint window", ""},

		{"correcting an entry", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogUpdate, WorklogID: "500", Person: "acc-a", Hours: 4}, "", "carried over, an entry corrected"},
		{"removing an entry", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogDelete, WorklogID: "500"}, "", "carried over, an entry removed"},
		{"correcting an entry that is not there", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogUpdate, WorklogID: "999", Person: "acc-a", Hours: 4}, "no worklog entry 999", ""},
		{"removing with no entry named", sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogDelete}, "no worklog entry was named", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := proposal(t, c.req)
			if c.skip != "" {
				if len(cs.Changes) != 0 || len(cs.Skipped) != 1 {
					t.Fatalf("want one skip and no change, got %d changes and %d skips", len(cs.Changes), len(cs.Skipped))
				}
				if got := cs.Skipped[0].Reason; !strings.Contains(got, c.skip) {
					t.Errorf("reason = %q, want it to mention %q", got, c.skip)
				}
				return
			}
			if len(cs.Changes) != 1 || len(cs.Skipped) != 0 {
				t.Fatalf("want one change and no skip, got %d changes and %v", len(cs.Changes), cs.Skipped)
			}
			if got := cs.Changes[0].Reason; got != c.reason {
				t.Errorf("reason = %q, want %q", got, c.reason)
			}
		})
	}
}

func TestProposeCarriesTheGuardFromTheIssue(t *testing.T) {
	cs := proposal(t, sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpPointsSet, Points: num(3)})
	c := cs.Changes[0]
	if c.Guard.IssueID != "10001" || c.Guard.Status != "Done" || c.Guard.Was != nil {
		t.Errorf("guard = %+v, want the issue id, its status and an empty Was", c.Guard)
	}
	if c.Guard.Updated.IsZero() {
		t.Error("the guard must carry fields.updated")
	}
	if c.Field != pointsField || c.After != 3.0 || c.AfterLabel != "3" {
		t.Errorf("change = field %q after %v label %q", c.Field, c.After, c.AfterLabel)
	}
	if cs.Digest != sprint.Digest(cs.Changes) || cs.SprintJiraID != 744 || cs.Actor != "operator@example.com" {
		t.Errorf("change set header = %+v", cs)
	}
	if !cs.ExpiresAt.Equal(cs.BuiltAt.Add(sprint.ChangeSetLifetime)) {
		t.Errorf("expires %v, built %v: want fifteen minutes apart", cs.ExpiresAt, cs.BuiltAt)
	}
}

// The one relaxation of write-to-blank: a container's rollup may be set
// over a figure already there. What makes that safe is the guard, which
// has to carry the figure so the apply writes only where Jira still
// holds it; and a Task keeps the empty-only rule whatever it holds.
func TestProposeStoryRollupCarriesTheCurrentValue(t *testing.T) {
	cs := proposal(t,
		sprint.ChangeRequest{Key: "ABC-6", Op: sprint.OpPointsSet, Points: num(11)},
		sprint.ChangeRequest{Key: "ABC-4", Op: sprint.OpPointsSet, Points: num(3)},
		sprint.ChangeRequest{Key: "ABC-5", Op: sprint.OpPointsSet, Points: num(3)},
	)
	if len(cs.Changes) != 2 || len(cs.Skipped) != 1 {
		t.Fatalf("want two changes and one skip, got %d and %v", len(cs.Changes), cs.Skipped)
	}
	over := cs.Changes[0]
	if over.Key != "ABC-6" || over.Guard.Was != 8.0 || over.After != 11.0 {
		t.Errorf("a rollup over a figure must guard on that figure: was %v after %v", over.Guard.Was, over.After)
	}
	if over.Guard.IssueID != "10006" || over.Field != pointsField {
		t.Errorf("guard = %+v field %q", over.Guard, over.Field)
	}
	blank := cs.Changes[1]
	if blank.Key != "ABC-4" || blank.Guard.Was != nil {
		t.Errorf("a rollup over nothing guards on nothing, got was %v", blank.Guard.Was)
	}
	if cs.Skipped[0].Key != "ABC-5" || !strings.Contains(cs.Skipped[0].Reason, "already has 5 points") {
		t.Errorf("a Task with points is still refused, got %+v", cs.Skipped[0])
	}
}

// A Story that concluded cleanly is in no person's rows and raises no
// flag; it is in the report's own Stories section, and that is enough to
// propose its rollup against.
func TestProposeReachesAConcludedStory(t *testing.T) {
	in := base(t)
	in.BaseURL = "https://example.atlassian.net"
	withStory(t, &in, "ABC-S1", 8, "Done", sprintOpens.AddDate(0, 0, 5),
		child{key: "ABC-T1", status: "Done", points: 3, doneAt: sprintOpens.AddDate(0, 0, 3)},
		child{key: "ABC-T2", status: "Done", points: 5, doneAt: sprintOpens.AddDate(0, 0, 5)},
	)
	rep := sprint.Build(in)
	if len(rep.Stories) != 1 {
		t.Fatalf("the fixture should conclude one Story, got %d", len(rep.Stories))
	}
	for _, f := range rep.Flags {
		if f.Key == "ABC-S1" {
			t.Fatalf("the fixture's Story agrees with its work and should raise nothing, got %+v", f)
		}
	}
	cs := sprint.Propose(rep, in.Issues, sprint.ProposalInputs{
		Requests: []sprint.ChangeRequest{{Key: "ABC-S1", Op: sprint.OpPointsSet, Points: num(9)}},
		People:   in.People, Rules: in.Rules, PointsField: pointsID,
		SprintOpens: sprintOpens, SprintCloses: sprintCloses, Now: sprintCloses,
	})
	if len(cs.Changes) != 1 {
		t.Fatalf("want the rollup proposed, got skips %v", cs.Skipped)
	}
	if c := cs.Changes[0]; c.Guard.Was != 8.0 || c.After != 9.0 || c.URL == "" {
		t.Errorf("change = %+v", c)
	}
	// And still no logging time against it: a container is not carryover.
	cs = sprint.Propose(rep, in.Issues, sprint.ProposalInputs{
		Requests: []sprint.ChangeRequest{{Key: "ABC-S1", Op: sprint.OpWorklogAdd, Person: "a", Hours: 2}},
		People:   in.People, Rules: in.Rules, PointsField: pointsID,
		SprintOpens: sprintOpens, SprintCloses: sprintCloses, Now: sprintCloses,
	})
	if len(cs.Skipped) != 1 || !strings.Contains(cs.Skipped[0].Reason, "not open when the sprint closed") {
		t.Errorf("worklog on a Story should be refused, got %+v %+v", cs.Changes, cs.Skipped)
	}
}

func TestProposeAssigneeIsAnAccountID(t *testing.T) {
	cs := proposal(t, sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpAssigneeSet, Assignee: "acc-b"})
	c := cs.Changes[0]
	after, ok := c.After.(map[string]string)
	if !ok || after["accountId"] != "acc-b" || len(after) != 1 {
		t.Errorf("after = %#v, want exactly {accountId: acc-b}", c.After)
	}
	if c.Field != "assignee" || c.AfterLabel != "Person B" {
		t.Errorf("field %q label %q", c.Field, c.AfterLabel)
	}
}

// The body for an add is what the gate accepts: three named fields, the
// comment an ADF document naming one person, and no hours in the text.
func TestProposeWorklogAddBuildsTheEntry(t *testing.T) {
	cs := proposal(t, sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-b", Hours: 4.5, Note: "pairing"})
	c := cs.Changes[0]
	after, ok := c.After.(map[string]any)
	if !ok || len(after) != 3 {
		t.Fatalf("after = %#v, want started, timeSpentSeconds and comment", c.After)
	}
	if after["timeSpentSeconds"] != 16200 {
		t.Errorf("seconds = %v, want 16200", after["timeSpentSeconds"])
	}
	// Undated, so dated at the close, in the layout Jira accepts.
	if after["started"] != "2026-01-19T09:00:00.000+0000" {
		t.Errorf("started = %v", after["started"])
	}
	raw, _ := json.Marshal(after["comment"])
	entry := jira.WorklogEntry{Comment: raw}
	if names := entry.Mentions(); len(names) != 1 || names[0] != "acc-b" {
		t.Errorf("the comment mentions %v, want exactly acc-b", names)
	}
	if strings.Contains(string(raw), "4.5") {
		t.Error("the hours must not be written into the comment text")
	}
	if !strings.Contains(string(raw), "pairing") {
		t.Error("the operator's note was dropped")
	}
	if c.Person != "acc-b" || c.PersonLabel != "Person B" || c.Hours != 4.5 || c.Field != "worklog" {
		t.Errorf("change = %+v", c)
	}
	if !strings.Contains(c.AfterLabel, "0.75 points") {
		t.Errorf("label %q should show the points beside the hours", c.AfterLabel)
	}
	ids, ok := c.Guard.Was.([]string)
	if ok && len(ids) != 0 {
		t.Errorf("Was = %v, want nothing held: the issue had no entries", ids)
	}
}

func TestProposeWorklogUpdateGuardsTheEntry(t *testing.T) {
	cs := proposal(t,
		sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogUpdate, WorklogID: "500", Person: "acc-a", Hours: 4},
		sprint.ChangeRequest{Key: "ABC-2", Op: sprint.OpWorklogAdd, Person: "acc-b", Hours: 2},
	)
	if len(cs.Changes) != 2 {
		t.Fatalf("want two changes, got %d and skips %v", len(cs.Changes), cs.Skipped)
	}
	upd := cs.Changes[0]
	if upd.WorklogID != "500" || upd.Guard.Entry == nil {
		t.Fatalf("update = %+v, want the entry guarded", upd)
	}
	if e := upd.Guard.Entry; e.ID != "500" || e.Seconds != 10800 || e.Updated.IsZero() || len(e.Mentions) != 1 {
		t.Errorf("entry guard = %+v", e)
	}
	if upd.Guard.Was != 10800 {
		t.Errorf("Was = %v, want the entry's seconds", upd.Guard.Was)
	}
	add := cs.Changes[1]
	if ids, _ := add.Guard.Was.([]string); len(ids) != 1 || ids[0] != "500" {
		t.Errorf("an add is guarded by the entries already held, got %v", add.Guard.Was)
	}
}

// One entry per person per issue. The second request for the same person
// is a skip, not a second entry.
func TestProposeOneEntryPerPerson(t *testing.T) {
	cs := proposal(t,
		sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 3},
		sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-a", Hours: 2},
		sprint.ChangeRequest{Key: "ABC-3", Op: sprint.OpWorklogAdd, Person: "acc-b", Hours: 1},
	)
	if len(cs.Changes) != 2 || len(cs.Skipped) != 1 {
		t.Fatalf("want two changes and one skip, got %d and %d", len(cs.Changes), len(cs.Skipped))
	}
	if !strings.Contains(cs.Skipped[0].Reason, "one entry per person") {
		t.Errorf("reason = %q", cs.Skipped[0].Reason)
	}
	if cs.Skipped[0].Person != "acc-a" {
		t.Errorf("the skip should name the person, got %q", cs.Skipped[0].Person)
	}
}

func TestProposeRefusesTheSameFieldTwice(t *testing.T) {
	cs := proposal(t,
		sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpPointsSet, Points: num(3)},
		sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpPointsSet, Points: num(5)},
	)
	if len(cs.Changes) != 1 || len(cs.Skipped) != 1 {
		t.Fatalf("want one change and one skip, got %d and %d", len(cs.Changes), len(cs.Skipped))
	}
	if cs.Changes[0].After != 3.0 {
		t.Error("the first value typed should be the one kept")
	}
}

func TestProposeWithNothingAskedIsEmptyNotNil(t *testing.T) {
	cs := proposal(t)
	if cs.Changes == nil || cs.Skipped == nil {
		t.Error("changes and skipped must be arrays in the JSON, not null")
	}
	if cs.Digest == "" {
		t.Error("an empty proposal still has a digest")
	}
}
