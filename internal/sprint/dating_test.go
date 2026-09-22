package sprint_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The sprint every test below is written against: it opened on the 5th
// and was completed on the 19th. Nothing outside that fortnight is this
// sprint's work, however Jira's sprint field labels it.
var (
	sprintOpens  = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	sprintCloses = time.Date(2026, 1, 19, 17, 0, 0, 0, time.UTC)
)

// statusIDs are the ids the fixtures use, and the categories they sit in.
// Real ids are site-specific numbers; these only have to be consistent.
const (
	idToDo       = "1"
	idInProgress = "2"
	idDone       = "3"
)

var categories = map[string]string{
	idToDo:       "new",
	idInProgress: "indeterminate",
	idDone:       "done",
}

// fixture builds one issue through the JSON decoder rather than as a
// literal, because that is where raw custom fields are picked up - a
// literal would carry no story points at all.
type fixture struct {
	key       string
	issueType string
	status    string
	assignee  string
	points    any // nil for "never estimated", which is not the same as zero
	estimate  any
	created   time.Time
	resolved  time.Time
	worklog   []logged
	links     []link
}

type logged struct {
	who     string
	at      time.Time
	seconds int
}

type link struct {
	kind      string
	key       string
	issueType string
	status    string
}

const (
	pointsID   = "customfield_10016"
	estimateID = "customfield_10111"
	sprintID   = "customfield_10020"
)

func (f fixture) build(t *testing.T) jira.Issue {
	t.Helper()
	created := f.created
	if created.IsZero() {
		created = sprintOpens.AddDate(0, 0, -2)
	}
	fields := map[string]any{
		"summary":   "Something",
		"issuetype": map[string]any{"name": f.issueType},
		"status":    map[string]any{"name": f.status, "id": statusID(f.status)},
		"created":   created.Format(time.RFC3339),
	}
	if f.assignee != "" {
		fields["assignee"] = map[string]any{"accountId": f.assignee, "displayName": "Person " + f.assignee}
	}
	if f.points != nil {
		fields[pointsID] = f.points
	}
	if f.estimate != nil {
		fields[estimateID] = f.estimate
	}
	if !f.resolved.IsZero() {
		fields["resolutiondate"] = f.resolved.Format(time.RFC3339)
	}
	if len(f.worklog) > 0 {
		entries := make([]map[string]any, 0, len(f.worklog))
		total := 0
		for _, w := range f.worklog {
			total += w.seconds
			entries = append(entries, map[string]any{
				"started":          w.at.Format(time.RFC3339),
				"timeSpentSeconds": w.seconds,
				"author":           map[string]any{"accountId": w.who, "displayName": "Person " + w.who},
			})
		}
		fields["timespent"] = total
		fields["worklog"] = map[string]any{"total": len(entries), "maxResults": 20, "worklogs": entries}
	}
	if len(f.links) > 0 {
		links := make([]map[string]any, 0, len(f.links))
		for _, l := range f.links {
			links = append(links, map[string]any{
				"type": map[string]any{"name": l.kind},
				"outwardIssue": map[string]any{
					"key": l.key,
					"fields": map[string]any{
						"summary":   "Underneath",
						"status":    map[string]any{"name": l.status, "id": statusID(l.status)},
						"issuetype": map[string]any{"name": l.issueType},
					},
				},
			})
		}
		fields["issuelinks"] = links
	}

	b, err := json.Marshal(map[string]any{"id": f.key, "key": f.key, "fields": fields})
	if err != nil {
		t.Fatalf("encoding %s: %v", f.key, err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatalf("decoding %s: %v", f.key, err)
	}
	return is
}

func statusID(name string) string {
	switch name {
	case "Done", "Released":
		return idDone
	case "In Progress":
		return idInProgress
	default:
		return idToDo
	}
}

// moved is one status transition, in the shape the changelog gives.
func moved(at time.Time, from, to string) jira.StatusChange {
	return jira.StatusChange{
		At: at, From: from, To: to,
		FromID: statusID(from), ToID: statusID(to),
	}
}

// base is a sprint with nothing in it, ready to have issues added.
func base(t *testing.T) sprint.Inputs {
	t.Helper()
	return sprint.Inputs{
		Sprint: jira.Sprint{
			ID: 744, Name: "Sprint 21", Number: 21, State: "closed",
			StartDate:    jira.Time{Time: sprintOpens},
			EndDate:      jira.Time{Time: sprintCloses.AddDate(0, 0, -1)},
			CompleteDate: jira.Time{Time: sprintCloses},
		},
		People: []store.Person{
			{AccountID: "a", Name: "Person a", Baseline: 10, Active: true},
			{AccountID: "b", Name: "Person b", Baseline: 10, Active: true},
		},
		Rules: sprint.Rules{
			Done:              []string{"Released", "Done"},
			Container:         []string{"Story"},
			ExcludedFromSayDo: []string{"Epic"},
		},
		PointsField:      pointsID,
		EstimateField:    estimateID,
		SprintField:      sprintID,
		StatusCategories: categories,
		StoryLinkTypes:   sprint.DefaultStoryLinkTypes,
		HoursPerPoint:    6,
		SprintLengthDays: 10,
		Changes:          map[string][]jira.StatusChange{},
		Linked:           map[string]jira.Issue{},
	}
}

func rowFor(t *testing.T, rows []sprint.Row, key string) sprint.Row {
	t.Helper()
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("%s is not among the %d rows", key, len(rows))
	return sprint.Row{}
}

// ---- when work finished ----------------------------------------------

// The bug this whole mechanism exists for: Jira's sprint field is
// cumulative, so a ticket that passed through four sprints came back on
// all four searches and every one of them reported it as delivered.
func TestOnlyTheSprintWorkFinishedInCountsIt(t *testing.T) {
	inside := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 5.0}
	after := fixture{key: "ABC-2", issueType: "Task", status: "Done", assignee: "a", points: 8.0}
	before := fixture{key: "ABC-3", issueType: "Task", status: "Done", assignee: "a", points: 3.0}

	in := base(t)
	in.Issues = []jira.Issue{inside.build(t), after.build(t), before.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}
	in.Changes["ABC-2"] = []jira.StatusChange{moved(sprintCloses.AddDate(0, 0, 4), "In Progress", "Done")}
	in.Changes["ABC-3"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, -6), "In Progress", "Done")}

	r := sprint.Build(in)

	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want only the 5 points that finished inside the sprint", r.Summary.DeliveredTotal)
	}
	if r.Summary.DoneCount != 1 {
		t.Errorf("done count = %d, want 1", r.Summary.DoneCount)
	}
	// Finished after the sprint closed: it was open at the end, so it is
	// carryover however done it looks now.
	if got := rowFor(t, r.Carry, "ABC-2").State; got != sprint.StateCarried {
		t.Errorf("ABC-2 state = %q, want %q", got, sprint.StateCarried)
	}
	// Finished before the sprint opened: neither delivered here nor
	// carried out of here, just still sitting on the board.
	if r.Summary.FinishedEarlier != 1 {
		t.Errorf("finished earlier = %d, want the one that was already done", r.Summary.FinishedEarlier)
	}
	for _, row := range r.Carry {
		if row.Key == "ABC-3" {
			t.Error("a ticket finished before the sprint opened is being reported as carryover")
		}
	}
}

// Finished, reopened, finished again. The sprint that saw the first
// attempt does not get to claim work that came back.
func TestReopenedWorkCountsWhereItFinallyFinished(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 5.0}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{
		moved(sprintOpens.AddDate(0, 0, -8), "In Progress", "Done"),
		moved(sprintOpens.AddDate(0, 0, -7), "Done", "In Progress"),
		moved(sprintOpens.AddDate(0, 0, 4), "In Progress", "Done"),
	}

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want the sprint the final transition fell in to count it", r.Summary.DeliveredTotal)
	}

	// The same issue read from the earlier sprint's point of view: it was
	// done there once, and must not be counted there.
	earlier := base(t)
	earlier.Sprint.StartDate = jira.Time{Time: sprintOpens.AddDate(0, 0, -14)}
	earlier.Sprint.CompleteDate = jira.Time{Time: sprintOpens.AddDate(0, 0, -1)}
	earlier.Sprint.EndDate = earlier.Sprint.CompleteDate
	earlier.Issues = in.Issues
	earlier.Changes = in.Changes

	if got := sprint.Build(earlier).Summary.DeliveredTotal; got != 0 {
		t.Errorf("earlier sprint delivered = %v, want nothing for work that came back", got)
	}
}

// A team with several done statuses moves a ticket on days later, often
// in the next sprint. It concluded when it first reached one of them.
func TestSteppingThroughDoneStatusesDatesFromTheFirst(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 5.0}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{
		moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Released"),
		moved(sprintCloses.AddDate(0, 0, 5), "Released", "Done"),
	}

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want the sprint that got it to the first done status", r.Summary.DeliveredTotal)
	}
}

// Nineteen of forty-five finished issues in one real sprint had no
// resolution date at all, because the status their team calls finished
// is a release gate that sets no resolution. The changelog answers it.
func TestDoneTransitionCountsWithNoResolutionDate(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Released", assignee: "a", points: 5.0}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 2), "In Progress", "Released")}

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want a done transition to count without a resolution date", r.Summary.DeliveredTotal)
	}
	if got := rowFor(t, r.People[0].Rows, "ABC-1").State; got != sprint.StateConcluded {
		t.Errorf("state = %q, want %q", got, sprint.StateConcluded)
	}
}

// Work finished in the few minutes between one sprint being completed and
// the next being started has to land somewhere.
func TestWorkFinishedInTheHandoverGapCounts(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 5.0}
	in := base(t)
	in.PreviousClose = sprintOpens.Add(-7 * time.Minute)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.Add(-4*time.Minute), "In Progress", "Done")}

	if got := sprint.Build(in).Summary.DeliveredTotal; got != 5 {
		t.Errorf("delivered = %v, want the gap between two sprints not to be a hole work falls into", got)
	}
}

// ---- carryover -------------------------------------------------------

// Carryover is work somebody could not finish. Work nobody picked up is
// also open at the end, and reporting the two as one reads as though the
// sprint attempted something it never began.
func TestCarryoverSeparatesWorkNobodyStarted(t *testing.T) {
	started := fixture{key: "ABC-1", issueType: "Task", status: "In Progress", assignee: "a", points: 5.0}
	untouched := fixture{key: "ABC-2", issueType: "Task", status: "To Do", assignee: "b", points: 3.0}

	in := base(t)
	in.Issues = []jira.Issue{started.build(t), untouched.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 1), "To Do", "In Progress")}
	in.Changes["ABC-2"] = nil

	r := sprint.Build(in)
	if r.Summary.CarriedOver != 2 {
		t.Errorf("carried over = %d, want both", r.Summary.CarriedOver)
	}
	if r.Summary.CarriedActive != 1 || r.Summary.NeverStarted != 1 {
		t.Errorf("active %d never started %d, want one of each",
			r.Summary.CarriedActive, r.Summary.NeverStarted)
	}
	if !rowFor(t, r.Carry, "ABC-1").Active {
		t.Error("a ticket somebody moved into progress is reported as never started")
	}
	if rowFor(t, r.Carry, "ABC-2").Active {
		t.Error("a ticket that sat in To Do all sprint is reported as worked on")
	}
}

// ---- splitting a ticket between sprints -------------------------------

// The invariant that matters: however a ticket is divided, the divisions
// add up to the ticket and never more. Anything else pays twice for the
// same work, which is the bug in a different costume.
func TestSplitNeverExceedsTheTicket(t *testing.T) {
	// Worked on across three sprints, nothing logged anywhere - which is
	// most of them, because logging time is a new habit here.
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 9.0}
	is := f.build(t)
	is.Raw[sprintID] = json.RawMessage(sprintsOn(t,
		[2]time.Time{sprintOpens.AddDate(0, 0, -28), sprintOpens.AddDate(0, 0, -15)},
		[2]time.Time{sprintOpens.AddDate(0, 0, -14), sprintOpens.AddDate(0, 0, -1)},
		[2]time.Time{sprintOpens, sprintCloses},
	))

	changes := []jira.StatusChange{
		moved(sprintOpens.AddDate(0, 0, -27), "To Do", "In Progress"),
		moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done"),
	}

	total := 0.0
	for _, w := range [][2]time.Time{
		{sprintOpens.AddDate(0, 0, -28), sprintOpens.AddDate(0, 0, -15)},
		{sprintOpens.AddDate(0, 0, -14), sprintOpens.AddDate(0, 0, -1)},
		{sprintOpens, sprintCloses},
	} {
		in := base(t)
		in.Sprint.StartDate = jira.Time{Time: w[0]}
		in.Sprint.EndDate = jira.Time{Time: w[1]}
		in.Sprint.CompleteDate = jira.Time{Time: w[1]}
		in.Issues = []jira.Issue{is}
		in.Changes["ABC-1"] = changes
		total += sprint.Build(in).Summary.DeliveredTotal
	}

	if total > 9.0+0.01 {
		t.Errorf("the three sprints between them counted %v of a 9 point ticket", total)
	}
	if total < 9.0-0.01 {
		t.Errorf("the three sprints between them counted only %v of a 9 point ticket", total)
	}
}

// A sprint that never picked the ticket up did none of it, so it takes no
// share however long the ticket sat on its board.
func TestASprintThatNeverTouchedItTakesNoShare(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "In Progress", assignee: "a", points: 6.0}
	is := f.build(t)
	is.Raw[sprintID] = json.RawMessage(sprintsOn(t,
		[2]time.Time{sprintOpens.AddDate(0, 0, -14), sprintOpens.AddDate(0, 0, -1)},
		[2]time.Time{sprintOpens, sprintCloses},
	))

	in := base(t)
	in.Sprint.StartDate = jira.Time{Time: sprintOpens.AddDate(0, 0, -14)}
	in.Sprint.CompleteDate = jira.Time{Time: sprintOpens.AddDate(0, 0, -1)}
	in.Sprint.EndDate = in.Sprint.CompleteDate
	in.Issues = []jira.Issue{is}
	// Only picked up in the second of the two sprints.
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 2), "To Do", "In Progress")}

	if got := sprint.Build(in).Summary.DeliveredTotal; got != 0 {
		t.Errorf("delivered = %v, want nothing for a sprint the ticket only sat in", got)
	}
}

// Where time has been logged it is a direct record of where the work
// went, so it decides the split rather than an equal share.
func TestLoggedTimeDecidesTheSplitAndTheEarlierSprintIsPaidBack(t *testing.T) {
	f := fixture{
		key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 10.0,
		worklog: []logged{
			// Six hours is one point, so four points were logged before
			// this sprint opened and belong to the sprint that ran then.
			{who: "b", at: sprintOpens.AddDate(0, 0, -5), seconds: 4 * 6 * 3600},
			{who: "b", at: sprintOpens.AddDate(0, 0, 2), seconds: 2 * 6 * 3600},
		},
	}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 4), "In Progress", "Done")}

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 6 {
		t.Errorf("delivered = %v, want 10 less the 4 points an earlier sprint was already credited", r.Summary.DeliveredTotal)
	}
	if r.Summary.PriorPointsDeducted != 4 {
		t.Errorf("prior points = %v, want the deduction reported rather than folded in", r.Summary.PriorPointsDeducted)
	}
}

// ---- attribution ------------------------------------------------------

// A ticket that spans sprints sits with whoever it was handed to last,
// which is frequently not who did the work.
func TestPointsGoToWhoeverLoggedTheTime(t *testing.T) {
	f := fixture{
		key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 8.0,
		worklog: []logged{
			{who: "b", at: sprintOpens.AddDate(0, 0, 1), seconds: 3 * 3600},
			{who: "a", at: sprintOpens.AddDate(0, 0, 2), seconds: 1 * 3600},
		},
	}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}

	r := sprint.Build(in)
	got := map[string]float64{}
	for _, p := range r.People {
		got[p.AccountID] = p.Delivered
	}
	if got["b"] != 6 || got["a"] != 2 {
		t.Errorf("delivered a=%v b=%v, want the split to follow the log (6 to b, 2 to a)", got["a"], got["b"])
	}
}

// Most tickets have no log at all, and the assignee is the right answer
// for them rather than a compromise.
func TestWithNoLogTheAssigneeTakesThePoints(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 4.0}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}

	r := sprint.Build(in)
	for _, p := range r.People {
		if p.AccountID == "a" && p.Delivered != 4 {
			t.Errorf("delivered = %v, want the assignee credited when nobody logged time", p.Delivered)
		}
	}
	if n := flagCount(sprint.Build(in), sprint.FlagCarriedNoWorklog); n != 0 {
		t.Errorf("%d worklog flags for finished work; the flag is about carryover", n)
	}
}

// ---- sizing -----------------------------------------------------------

// Crediting zero for work that demonstrably happened is further from the
// truth than using the estimate somebody wrote down. It is never silent.
func TestTheEstimateIsUsedWhenTheActualIsEmptyAndIsFlagged(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", estimate: 3.0}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 3 {
		t.Errorf("delivered = %v, want the estimate rather than a zero", r.Summary.DeliveredTotal)
	}
	if n := flagCount(r, sprint.FlagEstimateFallback); n != 1 {
		t.Errorf("%d estimate flags, want the substitution named", n)
	}
}

// No actual, no estimate and no time logged is a data gap, not a zero.
func TestFinishedWithNothingToSizeItByIsFlagged(t *testing.T) {
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a"}
	in := base(t)
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}

	if n := flagCount(sprint.Build(in), sprint.FlagDoneNoEstimate); n != 1 {
		t.Errorf("%d flags, want the gap named once", n)
	}
}

// ---- the missing worklog prompt ---------------------------------------

func TestCarriedWorkWithNoTimeLoggedIsOneGroupedPrompt(t *testing.T) {
	in := base(t)
	for _, key := range []string{"ABC-1", "ABC-2", "ABC-3"} {
		f := fixture{key: key, issueType: "Task", status: "In Progress", assignee: "a", points: 2.0}
		in.Issues = append(in.Issues, f.build(t))
		in.Changes[key] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 1), "To Do", "In Progress")}
	}
	// Never picked up, so there is nothing for anybody to have logged.
	untouched := fixture{key: "ABC-9", issueType: "Task", status: "To Do", assignee: "b", points: 2.0}
	in.Issues = append(in.Issues, untouched.build(t))

	r := sprint.Build(in)
	var flag sprint.Flag
	for _, f := range r.Flags {
		if f.Kind == sprint.FlagCarriedNoWorklog {
			flag = f
		}
	}
	if flag.Kind == "" {
		t.Fatal("no prompt was raised for carried work with no time logged")
	}
	if n := flagCount(r, sprint.FlagCarriedNoWorklog); n != 1 {
		t.Errorf("%d prompts, want one grouped entry rather than one per ticket", n)
	}
	if len(flag.Keys) != 3 {
		t.Errorf("keys = %v, want the three that were worked on and not the one nobody touched", flag.Keys)
	}
	for _, k := range flag.Keys {
		if k == "ABC-9" {
			t.Error("a ticket nobody picked up is being asked for a worklog")
		}
	}
}

// While a sprint is running nobody is late logging anything yet.
func TestTheWorklogPromptWaitsForTheSprintToClose(t *testing.T) {
	in := base(t)
	in.Sprint.State = "active"
	in.Sprint.CompleteDate = jira.Time{}
	in.Sprint.EndDate = jira.Time{Time: time.Now().AddDate(0, 0, 3)}
	f := fixture{key: "ABC-1", issueType: "Task", status: "In Progress", assignee: "a", points: 2.0}
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(time.Now().AddDate(0, 0, -1), "To Do", "In Progress")}

	if n := flagCount(sprint.Build(in), sprint.FlagCarriedNoWorklog); n != 0 {
		t.Errorf("%d prompts on an open sprint, want none", n)
	}
}

// ---- capacity ---------------------------------------------------------

// Capacity is what the team committed to knowing what it knew. Taking
// unplanned absence off afterwards would erase the miss it caused.
func TestOnlyPlannedLeaveReducesCapacity(t *testing.T) {
	in := base(t)
	in.People = []store.Person{{AccountID: "a", Name: "Person a", Baseline: 10, Active: true}}
	in.Capacity = []store.Capacity{
		{SprintJiraID: 744, AccountID: "a", PlannedDaysOff: 2, UnplannedDaysOff: 3, Reviewed: true},
	}
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 5.0}
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}

	r := sprint.Build(in)
	p := r.People[0]
	if p.Capacity != 8 {
		t.Errorf("capacity = %v, want the baseline less planned leave only", p.Capacity)
	}
	if p.Delta != -3 {
		t.Errorf("delta = %v, want the shortfall to stay visible", p.Delta)
	}
	// Three days of unplanned absence against a shortfall of three: it
	// accounts for the gap, and it is reported rather than hidden inside
	// a smaller capacity.
	if p.ShortfallFromAbsence != 3 {
		t.Errorf("shortfall from absence = %v, want 3", p.ShortfallFromAbsence)
	}
	if r.Summary.ShortfallFromAbsence != 3 {
		t.Errorf("summary shortfall = %v, want the sprint total", r.Summary.ShortfallFromAbsence)
	}
}

// Absence cannot explain away more than actually went missing.
func TestAbsenceNeverExplainsMoreThanTheShortfall(t *testing.T) {
	in := base(t)
	in.People = []store.Person{{AccountID: "a", Name: "Person a", Baseline: 10, Active: true}}
	in.Capacity = []store.Capacity{
		{SprintJiraID: 744, AccountID: "a", UnplannedDaysOff: 6, Reviewed: true},
	}
	f := fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 9.0}
	in.Issues = []jira.Issue{f.build(t)}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(sprintOpens.AddDate(0, 0, 3), "In Progress", "Done")}

	if got := sprint.Build(in).People[0].ShortfallFromAbsence; got != 1 {
		t.Errorf("shortfall from absence = %v, want it capped at the 1 point actually missing", got)
	}
}

// sprintsOn encodes the cumulative sprint field, which carries the dates
// of every sprint an issue has been on the board of.
func sprintsOn(t *testing.T, windows ...[2]time.Time) []byte {
	t.Helper()
	entries := make([]map[string]any, 0, len(windows))
	for i, w := range windows {
		entries = append(entries, map[string]any{
			"id":           100 + i,
			"name":         "Sprint " + string(rune('1'+i)),
			"startDate":    w[0].Format(time.RFC3339),
			"endDate":      w[1].Format(time.RFC3339),
			"completeDate": w[1].Format(time.RFC3339),
		})
	}
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("encoding the sprint field: %v", err)
	}
	return b
}
