package sprint_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// movedBy is a status transition with the person who made it, which is
// what the assign step reads.
func movedBy(t *testing.T, who string, from, to string, day int) jira.StatusChange {
	t.Helper()
	c := moved(sprintOpens.AddDate(0, 0, day), from, to)
	if who != "" {
		c.Author = &jira.User{AccountID: who, DisplayName: "Person " + who}
	}
	return c
}

// closeoutFixture is one sprint with a row for every step: finished work
// with and without points and owners, carryover touched and untouched,
// Stories over sized and unsized work, and a note on one person.
func closeoutFixture(t *testing.T) (sprint.Report, sprint.CloseoutInputs) {
	t.Helper()
	in := base(t)
	in.BaseURL = "https://example.atlassian.net"
	in.Capacity = []store.Capacity{{SprintJiraID: 744, AccountID: "a", PlannedDaysOff: 1, Note: "two days at a conference", Reviewed: true}}

	// Finished, no points, an estimate, nobody assigned; moved to Done by b.
	in.Issues = append(in.Issues, fixture{key: "ABC-F1", issueType: "Task", status: "Done", estimate: 3}.build(t))
	in.Changes["ABC-F1"] = []jira.StatusChange{movedBy(t, "b", "In Progress", "Done", 7)}
	// Finished with points, nobody assigned, moved to Done by somebody off the roster.
	in.Issues = append(in.Issues, fixture{key: "ABC-F2", issueType: "Bug", status: "Done", points: 2}.build(t))
	in.Changes["ABC-F2"] = []jira.StatusChange{movedBy(t, "zz", "In Progress", "Done", 8)}
	// Finished, no points, no estimate, time logged by its owner: the row
	// the report credits nothing to and lists nowhere.
	in.Issues = append(in.Issues, fixture{key: "ABC-F3", issueType: "Task", status: "Done", assignee: "a",
		worklog: []logged{{who: "a", at: sprintOpens.AddDate(0, 0, 6), seconds: 6 * 3600}}}.build(t))
	in.Changes["ABC-F3"] = []jira.StatusChange{movedBy(t, "a", "In Progress", "Done", 9)}
	// Finished before this sprint opened: none of the close-out's business.
	in.Issues = append(in.Issues, fixture{key: "ABC-F4", issueType: "Task", status: "Done"}.build(t))
	in.Changes["ABC-F4"] = []jira.StatusChange{movedBy(t, "a", "In Progress", "Done", -10)}
	// Tidied from one Done status to another after b finished it: b is
	// still the finisher.
	in.Issues = append(in.Issues, fixture{key: "ABC-F5", issueType: "Task", status: "Released"}.build(t))
	in.Changes["ABC-F5"] = []jira.StatusChange{movedBy(t, "b", "In Progress", "Done", 4), movedBy(t, "a", "Done", "Released", 6)}

	// Carried and worked on, with an entry logged; carried and never touched.
	in.Issues = append(in.Issues, fixture{key: "ABC-C1", issueType: "Task", status: "In Progress", assignee: "a", points: 5,
		worklog: []logged{{who: "a", at: sprintOpens.AddDate(0, 0, 10), seconds: 3 * 3600}}}.build(t))
	in.Issues = append(in.Issues, fixture{key: "ABC-C2", issueType: "Task", status: "To Do", points: 2}.build(t))

	// Stories: concluded over sized work; concluded over unsized work; shut over open work.
	withStory(t, &in, "ABC-S1", 5, "Done", sprintOpens.AddDate(0, 0, 5),
		child{key: "ABC-T1", status: "Done", points: 3, doneAt: sprintOpens.AddDate(0, 0, 3)},
		child{key: "ABC-T2", status: "Done", points: 5, doneAt: sprintOpens.AddDate(0, 0, 5)},
	)
	withStory(t, &in, "ABC-S2", nil, "Done", sprintOpens.AddDate(0, 0, 8),
		child{key: "ABC-T3", status: "Done", points: 2, doneAt: sprintOpens.AddDate(0, 0, 8)},
		child{key: "ABC-T4", status: "Done", points: nil, doneAt: sprintOpens.AddDate(0, 0, 8)},
	)
	withStory(t, &in, "ABC-S3", 8, "Done", sprintOpens.AddDate(0, 0, 8),
		child{key: "ABC-T5", status: "In Progress", points: 3},
	)

	rep := sprint.Build(in)
	return rep, sprint.CloseoutInputs{
		Issues: in.Issues, Changes: in.Changes, People: in.People, Rules: in.Rules,
		BaseURL: in.BaseURL, Linked: in.Linked, StoryLinkTypes: in.StoryLinkTypes,
		PointsField: pointsID, EstimateField: estimateID, HoursPerPoint: 6,
	}
}

func keysOf[T any](rows []T, key func(T) string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, key(r))
	}
	return out
}

func TestCloseoutSizesWhatFinishedWithoutPoints(t *testing.T) {
	rep, in := closeoutFixture(t)
	m := sprint.Closeout(rep, in)

	got := keysOf(m.Size, func(r sprint.SizeRow) string { return r.Key })
	if strings.Join(got, ",") != "ABC-F1,ABC-F3,ABC-F5" {
		t.Fatalf("size rows = %v; want the finished tickets with an empty points field, whatever they were credited", got)
	}
	f1 := m.Size[0]
	if f1.Suggested == nil || *f1.Suggested != 3 || f1.Estimate == nil || *f1.Estimate != 3 {
		t.Errorf("F1 has an estimate of 3 and it should be the suggestion: %+v", f1)
	}
	if f1.Type != "Task" || f1.Status != "Done" || !strings.HasSuffix(f1.URL, "/browse/ABC-F1") {
		t.Errorf("F1 row = %+v", f1)
	}
	if f3 := m.Size[1]; f3.Suggested != nil || f3.Estimate != nil {
		t.Errorf("F3 has no estimate, so nothing is suggested: %+v", f3)
	}
}

func TestCloseoutAssignsWhatFinishedUnowned(t *testing.T) {
	rep, in := closeoutFixture(t)
	m := sprint.Closeout(rep, in)

	got := keysOf(m.Assign, func(r sprint.AssignRow) string { return r.Key })
	if strings.Join(got, ",") != "ABC-F1,ABC-F2,ABC-F5" {
		t.Fatalf("assign rows = %v", got)
	}
	f1, f2, f5 := m.Assign[0], m.Assign[1], m.Assign[2]
	if f1.SuggestedAccountID != "b" || f1.SuggestedLabel != "Person b" {
		t.Errorf("F1 was moved to Done by b, who is on the roster: %+v", f1)
	}
	if f2.SuggestedAccountID != "" || f2.SuggestedLabel != "" {
		t.Errorf("F2 was moved by somebody off the roster, so nobody is suggested: %+v", f2)
	}
	if f5.SuggestedAccountID != "b" {
		t.Errorf("F5 was finished by b and tidied by a; the finisher is b, got %q", f5.SuggestedAccountID)
	}
	roster := keysOf(f1.Candidates, func(c sprint.Assignable) string { return c.AccountID })
	if strings.Join(roster, ",") != "a,b" {
		t.Errorf("candidates should be the active roster, got %v", roster)
	}
}

func TestCloseoutListsEffortOnCarryoverThatWasWorkedOn(t *testing.T) {
	rep, in := closeoutFixture(t)
	m := sprint.Closeout(rep, in)

	if len(m.Effort) != 1 || m.Effort[0].Key != "ABC-C1" {
		t.Fatalf("effort rows = %+v; want the one carried ticket somebody picked up", m.Effort)
	}
	c1 := m.Effort[0]
	if c1.AssigneeAccountID != "a" || c1.AssigneeLabel != "Person a" || c1.HoursLogged != 3 {
		t.Errorf("C1 = %+v", c1)
	}
	if len(c1.Entries) != 1 || c1.Entries[0].Hours != 3 || c1.Entries[0].Author != "a" {
		t.Errorf("C1 entries = %+v", c1.Entries)
	}
	if m.HoursPerPoint != 6 {
		t.Errorf("hours per point = %v", m.HoursPerPoint)
	}
}

func TestCloseoutRollsUpStoriesWhoseWorkIsDone(t *testing.T) {
	rep, in := closeoutFixture(t)
	m := sprint.Closeout(rep, in)

	got := keysOf(m.Stories, func(r sprint.StoryRollup) string { return r.Key })
	if strings.Join(got, ",") != "ABC-S1,ABC-S2" {
		t.Fatalf("stories = %v; S3 still has work open and is not wrapping up", got)
	}
	s1, s2 := m.Stories[0], m.Stories[1]
	if !s1.SumKnown || s1.LinkedCount != 2 || s1.LinkedPoints != 8 || s1.Suggested == nil || *s1.Suggested != 8 {
		t.Errorf("S1 sits over 3 and 5, so the sum is 8 and known: %+v", s1)
	}
	if s1.OwnPoints == nil || *s1.OwnPoints != 5 {
		t.Errorf("S1 carries 5 of its own, shown beside the sum: %+v", s1)
	}
	if s2.SumKnown || s2.Suggested != nil || s2.OwnPoints != nil || s2.LinkedCount != 2 || s2.LinkedPoints != 2 {
		t.Errorf("S2 has an unsized item beneath it, so no sum is suggested: %+v", s2)
	}
}

func TestCloseoutNamesTheMeasuredPeopleWithTheirNotes(t *testing.T) {
	rep, in := closeoutFixture(t)
	m := sprint.Closeout(rep, in)

	if m.SprintJiraID != 744 || m.Sprint != 21 {
		t.Errorf("sprint = %d / %d", m.SprintJiraID, m.Sprint)
	}
	got := keysOf(m.People, func(p sprint.CloseoutPerson) string { return p.AccountID })
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("people = %v", got)
	}
	if m.People[0].Note != "two days at a conference" || m.People[1].Note != "" {
		t.Errorf("notes = %q / %q", m.People[0].Note, m.People[1].Note)
	}
}

// Every list crosses to a browser, so an empty checklist is five empty
// arrays and never a null.
func TestCloseoutOfNothingIsEmptyNotNull(t *testing.T) {
	b, err := json.Marshal(sprint.Closeout(sprint.Report{}, sprint.CloseoutInputs{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"size":[]`, `"assign":[]`, `"effort":[]`, `"stories":[]`, `"people":[]`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("want %s in %s", key, b)
		}
	}
}

// The draft the store keeps is the request the sprint package proposes
// from, spelled twice because the store sits beneath this package. This
// is what holds the two spellings to each other.
func TestDraftRequestMirrorsChangeRequest(t *testing.T) {
	three := 3.0
	req := sprint.ChangeRequest{Key: "ABC-1", Op: sprint.OpWorklogAdd, Points: &three, Assignee: "acc-a",
		Person: "acc-b", Hours: 1.5, Started: sprintCloses, Note: "n", WorklogID: "9"}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var d store.DraftRequest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("a ChangeRequest has a field the DraftRequest does not: %v", err)
	}
	back, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(b) {
		t.Errorf("the two shapes differ:\n%s\n%s", b, back)
	}
}
