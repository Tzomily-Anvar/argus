package sprint_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The points field id differs on every Jira site, so a report is always
// told which one to read. Any id will do in a test.
const pointsField = "customfield_10016"

// issue builds one through the JSON decoder rather than as a literal,
// because that is where the raw custom fields are picked up - a literal
// would carry no story points at all.
func issue(t *testing.T, key, issueType, status, assignee string, points float64) jira.Issue {
	t.Helper()
	body := map[string]any{
		"key": key,
		"fields": map[string]any{
			"summary":   "Something",
			"issuetype": map[string]any{"name": issueType},
			"status":    map[string]any{"name": status},
			"assignee":  map[string]any{"accountId": assignee, "displayName": "Person " + assignee},
			"created":   "2026-01-01T09:00:00.000+0000",
			// Finished inside the sprint under test. Completion is dated
			// from the changelog where there is one and from here where
			// there is not, and an undated finish would land before the
			// sprint opened rather than in it.
			"resolutiondate": "2026-01-10T09:00:00.000+0000",
			pointsField:      points,
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the test issue: %v", err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatalf("decoding the test issue: %v", err)
	}
	return is
}

func inputs(t *testing.T) sprint.Inputs {
	t.Helper()
	starts := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	return sprint.Inputs{
		Sprint: jira.Sprint{
			ID: 744, Name: "Sprint 21", Number: 21, State: "closed",
			StartDate: jira.Time{Time: starts},
			EndDate:   jira.Time{Time: starts.AddDate(0, 0, 14)},
		},
		Issues: []jira.Issue{issue(t, "ABC-1", "Task", "Done", "acc-a", 5)},
		People: []store.Person{
			{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true},
		},
		Rules:            sprint.Rules{Done: []string{"Done"}, Container: []string{"Story"}},
		PointsField:      pointsField,
		SprintLengthDays: 10,
	}
}

func flagCount(r sprint.Report, kind string) int {
	n := 0
	for _, f := range r.Flags {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

// The three states are the whole point: an empty capacity table means two
// different things, and the report has to say which.
func TestCapacityReviewStates(t *testing.T) {
	reviewedAt := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)

	t.Run("nobody has looked at it", func(t *testing.T) {
		r := sprint.Build(inputs(t))
		if r.CapacityReview.State != sprint.ReviewNone {
			t.Errorf("state = %q, want %q", r.CapacityReview.State, sprint.ReviewNone)
		}
		if r.CapacityReview.ReviewedAt != nil {
			t.Errorf("an unreviewed sprint should carry no timestamp, got %v", r.CapacityReview.ReviewedAt)
		}
		if flagCount(r, sprint.FlagCapacityUnreviewed) != 1 {
			t.Errorf("want one unreviewed flag, got %d", flagCount(r, sprint.FlagCapacityUnreviewed))
		}
	})

	// The case that could not be recorded before: everyone was here.
	t.Run("reviewed, everyone was available", func(t *testing.T) {
		in := inputs(t)
		in.CapacityReviewedAt = reviewedAt

		r := sprint.Build(in)
		if r.CapacityReview.State != sprint.ReviewNoAdjustments {
			t.Errorf("state = %q, want %q", r.CapacityReview.State, sprint.ReviewNoAdjustments)
		}
		if r.CapacityReview.Adjusted != 0 {
			t.Errorf("nobody was away, so adjusted should be 0, got %d", r.CapacityReview.Adjusted)
		}
		if r.CapacityReview.ReviewedAt == nil || !r.CapacityReview.ReviewedAt.Equal(reviewedAt) {
			t.Errorf("review timestamp lost: %v", r.CapacityReview.ReviewedAt)
		}
		// A confirmed sprint should stop being nagged about.
		if n := flagCount(r, sprint.FlagCapacityUnreviewed); n != 0 {
			t.Errorf("a reviewed sprint should raise no unreviewed flags, got %d", n)
		}
		// Nobody away means capacity is the baseline, untouched.
		if r.People[0].Capacity != 10 {
			t.Errorf("capacity = %v, want the full baseline of 10", r.People[0].Capacity)
		}
	})

	t.Run("reviewed, somebody was away", func(t *testing.T) {
		in := inputs(t)
		in.CapacityReviewedAt = reviewedAt
		in.Capacity = []store.Capacity{
			{SprintJiraID: 744, AccountID: "acc-a", PlannedDaysOff: 2, Reviewed: true},
		}

		r := sprint.Build(in)
		if r.CapacityReview.State != sprint.ReviewAdjusted {
			t.Errorf("state = %q, want %q", r.CapacityReview.State, sprint.ReviewAdjusted)
		}
		if r.CapacityReview.Adjusted != 1 {
			t.Errorf("adjusted = %d, want 1", r.CapacityReview.Adjusted)
		}
		// Ten points over ten days, two days away: eight.
		if r.People[0].Capacity != 8 {
			t.Errorf("capacity = %v, want 8", r.People[0].Capacity)
		}
	})

	// Rows entered but never confirmed is a half-finished job, and must
	// not read as a completed review.
	t.Run("adjustments entered but not confirmed", func(t *testing.T) {
		in := inputs(t)
		in.Capacity = []store.Capacity{
			{SprintJiraID: 744, AccountID: "acc-a", UnplannedDaysOff: 1, Reviewed: true},
		}

		r := sprint.Build(in)
		if r.CapacityReview.State != sprint.ReviewNone {
			t.Errorf("state = %q, want %q", r.CapacityReview.State, sprint.ReviewNone)
		}
		if r.CapacityReview.Adjusted != 1 {
			t.Errorf("adjusted = %d, want 1", r.CapacityReview.Adjusted)
		}
	})
}

// ---- who is measured, and who merely delivered ------------------------

// Three standings, and the difference between them is the whole opt-in
// model. The one thing none of them changes is the sprint total: points
// delivered by anybody are points the sprint delivered.

func person(t *testing.T, r sprint.Report, accountID string) sprint.Person {
	t.Helper()
	for _, p := range r.People {
		if p.AccountID == accountID {
			return p
		}
	}
	t.Fatalf("%s is not in the report", accountID)
	return sprint.Person{}
}

func TestOptedOutIsKnownButNotMeasured(t *testing.T) {
	in := inputs(t)
	// On the roster with a baseline, deliberately opted out - a manager,
	// or somebody on loan to another team.
	in.People = []store.Person{
		{AccountID: "acc-a", Name: "Person acc-a", Baseline: 10, Active: false},
	}

	r := sprint.Build(in)
	p := person(t, r, "acc-a")

	if !p.OnRoster {
		t.Error("somebody on the roster is reported as not on it")
	}
	if p.Measured {
		t.Error("an opted-out person is being measured")
	}
	if p.Delivered != 5 {
		t.Errorf("delivered = %v, want the 5 points they delivered", p.Delivered)
	}
	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("sprint delivered = %v, want opting out not to remove delivery from the total",
			r.Summary.DeliveredTotal)
	}
	// Measured against nothing means contributing nothing to measure
	// against: a capacity total mixing the two would be delivery by eight
	// people against the capacity of five.
	if r.Summary.CapacityTotal != 0 || r.Summary.BaselineTotal != 0 {
		t.Errorf("baseline %v capacity %v, want an opted-out person left out of both",
			r.Summary.BaselineTotal, r.Summary.CapacityTotal)
	}
	if p.Delta != 0 {
		t.Errorf("delta = %v, want nothing claimed about somebody with no baseline in play", p.Delta)
	}
	// Opting somebody out is the answer to the off-roster flag, not a new
	// reason to raise it.
	if n := flagCount(r, sprint.FlagDeliveredOffRoster); n != 0 {
		t.Errorf("%d off-roster flags for somebody deliberately opted out", n)
	}
	if n := flagCount(r, sprint.FlagCapacityUnreviewed); n != 0 {
		t.Errorf("%d capacity flags for somebody who is not measured", n)
	}
}

// Somebody opted out who delivered nothing is not part of this sprint at
// all. Their row would be a name and five dashes.
func TestOptedOutWithNoDeliveryIsLeftOut(t *testing.T) {
	in := inputs(t)
	in.People = append(in.People, store.Person{
		AccountID: "acc-quiet", Name: "Quiet", Baseline: 10, Active: false,
	})

	r := sprint.Build(in)
	for _, p := range r.People {
		if p.AccountID == "acc-quiet" {
			t.Error("an opted-out person with no delivery is taking up a row")
		}
	}
}

// A contractor or another team's engineer. Their points count; there is
// simply no baseline of ours to compare them to, and that is worth saying
// once.
func TestDeliveryFromSomebodyOffTheRoster(t *testing.T) {
	in := inputs(t)
	in.Issues = append(in.Issues, issue(t, "ABC-2", "Task", "Done", "acc-outside", 3))

	r := sprint.Build(in)
	p := person(t, r, "acc-outside")

	if p.OnRoster || p.Measured {
		t.Errorf("acc-outside: onRoster %v measured %v, want neither", p.OnRoster, p.Measured)
	}
	if p.Delivered != 3 {
		t.Errorf("delivered = %v, want 3", p.Delivered)
	}
	if r.Summary.DeliveredTotal != 8 {
		t.Errorf("sprint delivered = %v, want all 8 points counted", r.Summary.DeliveredTotal)
	}
	if n := flagCount(r, sprint.FlagDeliveredOffRoster); n != 1 {
		t.Errorf("%d off-roster flags, want exactly one", n)
	}
}

// Opted in with nothing to be measured against is a half-finished setup
// rather than a decision, so it is named as one.
func TestOptedInWithNoBaselineIsFlagged(t *testing.T) {
	in := inputs(t)
	in.People = []store.Person{
		{AccountID: "acc-a", Name: "Person acc-a", Baseline: 0, Active: true},
	}

	r := sprint.Build(in)
	if person(t, r, "acc-a").Measured {
		t.Error("somebody with no baseline is being measured against one")
	}
	if n := flagCount(r, sprint.FlagNoBaseline); n != 1 {
		t.Errorf("%d no-baseline flags, want exactly one", n)
	}
	// They are on the roster, so this is not the off-roster problem.
	if n := flagCount(r, sprint.FlagDeliveredOffRoster); n != 0 {
		t.Errorf("%d off-roster flags for somebody who is on it", n)
	}
}

// The badge is the summary of the table beneath it, so it has to count
// the same people. A capacity row outlives somebody being opted out, so
// counting stored rows would have the badge claim somebody was away who
// the panel no longer lists at all.
func TestAwayCountIgnoresPeopleWhoAreNotMeasured(t *testing.T) {
	in := inputs(t)
	in.People = []store.Person{
		{AccountID: "acc-a", Name: "Person acc-a", Baseline: 10, Active: false},
	}
	in.CapacityReviewedAt = time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	// A row left behind from when they were measured.
	in.Capacity = []store.Capacity{
		{SprintJiraID: 744, AccountID: "acc-a", PlannedDaysOff: 3, Reviewed: true},
	}

	r := sprint.Build(in)
	if r.CapacityReview.Adjusted != 0 {
		t.Errorf("adjusted = %d, want nobody counted away who is not measured", r.CapacityReview.Adjusted)
	}
	if r.CapacityReview.State != sprint.ReviewNoAdjustments {
		t.Errorf("state = %q, want %q", r.CapacityReview.State, sprint.ReviewNoAdjustments)
	}
}

// A day off costs one point, whatever the baseline.
//
// The earlier rule divided the baseline by the sprint length, which made
// a day away cheaper for anyone part-time: six points less three days
// came out at 4.2 rather than 3. A point is a working day by the team's
// own convention, so a day away is a point, and this goes through Build
// rather than restating the arithmetic.
func TestADayOffCostsOnePointWhateverTheBaseline(t *testing.T) {
	for _, c := range []struct {
		name     string
		baseline float64
		daysOff  float64
		want     float64
	}{
		{"part-time, the case that exposed it", 6, 3, 3},
		{"full baseline", 10, 2, 8},
		{"away longer than the baseline is clamped at zero", 6, 9, 0},
		{"nobody away", 6, 0, 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := inputs(t)
			in.People = []store.Person{
				{AccountID: "acc-a", Name: "Person acc-a", Baseline: c.baseline, Active: true},
			}
			in.Capacity = []store.Capacity{
				{SprintJiraID: 744, AccountID: "acc-a", PlannedDaysOff: c.daysOff},
			}

			r := sprint.Build(in)
			var got float64
			found := false
			for _, p := range r.People {
				if p.AccountID == "acc-a" {
					got, found = p.Capacity, true
				}
			}
			if !found {
				t.Fatal("the person under test is missing from the report")
			}
			if got != c.want {
				t.Errorf("baseline %v less %v days = %v, want %v",
					c.baseline, c.daysOff, got, c.want)
			}
		})
	}
}

// Under the share rule a day off costs the baseline's share of one sprint
// day, so a small baseline spread across the sprint loses little to a
// single day away. Unplanned absence is priced the same way when it
// explains a shortfall.
func TestADayOffCanCostAShareOfTheBaseline(t *testing.T) {
	for _, c := range []struct {
		name          string
		baseline      float64
		planned       float64
		unplanned     float64
		delivered     float64
		wantCapacity  float64
		wantShortfall float64
	}{
		{"a lead on three points, one day off", 3, 1, 0, 2.7, 2.7, 0},
		{"full baseline, two days", 10, 2, 0, 8, 8, 0},
		{"unplanned explains the shortfall at the same price", 10, 0, 2, 7, 10, 2},
		{"unplanned never explains more than went missing", 10, 0, 5, 9, 10, 1},
		{"away longer than the sprint is clamped at zero", 6, 12, 0, 0, 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := inputs(t)
			in.AbsenceCost = config.AbsenceCostsShare
			in.SprintLengthDays = 10
			in.People = []store.Person{{AccountID: "acc-a", Name: "Person acc-a", Baseline: c.baseline, Active: true}}
			in.Capacity = []store.Capacity{{SprintJiraID: 744, AccountID: "acc-a", PlannedDaysOff: c.planned, UnplannedDaysOff: c.unplanned}}
			in.Issues = []jira.Issue{issue(t, "ABC-1", "Task", "Done", "acc-a", c.delivered)}

			p := person(t, sprint.Build(in), "acc-a")
			if p.Capacity != c.wantCapacity {
				t.Errorf("capacity = %v, want %v", p.Capacity, c.wantCapacity)
			}
			if p.ShortfallFromAbsence != c.wantShortfall {
				t.Errorf("shortfall from absence = %v, want %v", p.ShortfallFromAbsence, c.wantShortfall)
			}
		})
	}
}

// A flag's JQL link has to open in the issue navigator.
//
// The REST API accepts customfield_10033, and so does the REST search,
// which is why this was not noticed until somebody followed a link: the
// navigator refuses that form and says the field does not exist.
func TestFlagJQLUsesAFieldFormTheNavigatorAccepts(t *testing.T) {
	in := inputs(t)
	in.PointsField = "customfield_10033"
	// A finished task with no points is what raises the flag.
	in.Issues = []jira.Issue{issue(t, "ABC-2", "Task", "Done", "acc-a", 0)}

	r := sprint.Build(in)

	var link string
	for _, f := range r.Flags {
		if f.Kind == sprint.FlagDoneNoEstimate {
			link = f.JQL
		}
	}
	if link == "" {
		t.Fatal("no done-without-estimate flag was raised")
	}
	if strings.Contains(link, "customfield_10033") {
		t.Errorf("the link uses the REST field id, which the navigator rejects:\n  %s", link)
	}
	if !strings.Contains(link, "cf%5B10033%5D") && !strings.Contains(link, "cf[10033]") {
		t.Errorf("expected the cf[10033] form in:\n  %s", link)
	}
}
