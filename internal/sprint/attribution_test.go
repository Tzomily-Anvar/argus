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

// workedBy appends one worklog entry, dated inside the test sprint, whose
// comment names people. Each name is a mention node followed by whatever
// text was given for it, so "acc-a", " 3h" reads as "@Person A 3h".
func workedBy(is *jira.Issue, author string, hours float64, named ...string) {
	if len(named)%2 != 0 {
		panic("workedBy wants pairs of account id and trailing text")
	}
	nodes := make([]string, 0, len(named))
	for i := 0; i < len(named); i += 2 {
		nodes = append(nodes, `{"type":"mention","attrs":{"id":"`+named[i]+`","text":"@Person"}}`)
		if named[i+1] != "" {
			nodes = append(nodes, `{"type":"text","text":"`+named[i+1]+`"}`)
		}
	}
	var comment json.RawMessage
	if len(nodes) > 0 {
		comment = json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[` +
			strings.Join(nodes, ",") + `]}]}`)
	}
	is.Fields.Worklog.Entries = append(is.Fields.Worklog.Entries, jira.WorklogEntry{
		Started: jira.Time{Time: time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)},
		Seconds: int(hours * 3600),
		Author:  &jira.User{AccountID: author, DisplayName: "Person " + author},
		Comment: comment,
	})
	is.Fields.Worklog.Total = len(is.Fields.Worklog.Entries)
	is.Fields.TimeSpent += int(hours * 3600)
}

// attributed builds a report over one finished six-point ticket, assigned
// to the lead, with the given worklog, under the given attribution mode.
func attributed(t *testing.T, mode string, log func(*jira.Issue)) sprint.Report {
	t.Helper()
	in := inputs(t)
	is := issue(t, "ABC-1", "Task", "Done", "acc-lead", 6)
	log(&is)
	in.Issues = []jira.Issue{is}
	in.People = []store.Person{
		{AccountID: "acc-lead", Name: "Lead", Baseline: 3, Active: true},
		{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true},
		{AccountID: "acc-b", Name: "Person B", Baseline: 10, Active: true},
	}
	in.WorklogAttribution = mode
	in.HoursPerPoint = 6
	in.HoursPerDay = 6
	return sprint.Build(in)
}

func deliveredBy(r sprint.Report, accountID string) float64 {
	for _, p := range r.People {
		if p.AccountID == accountID {
			return p.Delivered
		}
	}
	return 0
}

func expectDelivered(t *testing.T, r sprint.Report, want map[string]float64) {
	t.Helper()
	for id, pts := range want {
		if got := deliveredBy(r, id); got != pts {
			t.Errorf("%s delivered %.2f, want %.2f", id, got, pts)
		}
	}
}

// Under author attribution nothing changes: the lead logged it, the lead
// is credited, whoever the comment names.
func TestAuthorAttributionCreditsWhoeverLogged(t *testing.T) {
	r := attributed(t, config.AttributeToAuthor, func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-a", " 3h", "acc-b", " 3h") })
	expectDelivered(t, r, map[string]float64{"acc-lead": 6, "acc-a": 0, "acc-b": 0})
	if n := flagCount(r, sprint.FlagWorklogSplitEqually); n != 0 {
		t.Errorf("%d split flags under author attribution, want 0: mentions carry no meaning there", n)
	}
}

// Under mention attribution the one person named is credited, and the
// author - who only recorded it - is not.
func TestMentionAttributionCreditsTheOnePersonNamed(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-a", "") })
	expectDelivered(t, r, map[string]float64{"acc-a": 6, "acc-lead": 0})
	if r.Summary.UnattributedPoints != 0 {
		t.Errorf("unattributed = %.2f, want 0", r.Summary.UnattributedPoints)
	}
}

// Naming yourself, or naming nobody, is the author's own time.
func TestMentionAttributionFallsBackToTheAuthor(t *testing.T) {
	cases := map[string]func(*jira.Issue){
		"self":       func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-lead", "") },
		"no comment": func(is *jira.Issue) { workedBy(is, "acc-lead", 6) },
	}
	for name, log := range cases {
		r := attributed(t, config.AttributeToMention, log)
		if got := deliveredBy(r, "acc-lead"); got != 6 {
			t.Errorf("%s: lead delivered %.2f, want 6", name, got)
		}
	}
}

// Several names with a figure beside each divide the entry by those
// figures. The unit does not matter while it is the same for everyone,
// and is converted when it is not.
func TestSeveralNamesDivideByTheFiguresBesideThem(t *testing.T) {
	cases := map[string]struct {
		named []string
		want  map[string]float64
	}{
		"hours":            {[]string{"acc-a", ": 3h ", "acc-b", " 1h"}, map[string]float64{"acc-a": 4.5, "acc-b": 1.5}},
		"no unit, comma":   {[]string{"acc-a", " 3,5 ", "acc-b", " 1,5"}, map[string]float64{"acc-a": 4.2, "acc-b": 1.8}},
		"days and hours":   {[]string{"acc-a", " 0.5d ", "acc-b", " 1h"}, map[string]float64{"acc-a": 4.5, "acc-b": 1.5}},
		"points and hours": {[]string{"acc-a", " 0.5sp ", "acc-b", " 1h"}, map[string]float64{"acc-a": 4.5, "acc-b": 1.5}},
	}
	for name, c := range cases {
		r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 12, c.named...) })
		expectDelivered(t, r, c.want)
		if n := flagCount(r, sprint.FlagWorklogSplitEqually); n != 0 {
			t.Errorf("%s: %d split flags, want 0: the breakdown was complete", name, n)
		}
	}
}

// Several names and no figures divide equally, and say so.
func TestSeveralNamesWithoutFiguresDivideEquallyAndFlag(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 35, "acc-a", " and ", "acc-b", "") })
	expectDelivered(t, r, map[string]float64{"acc-a": 3, "acc-b": 3, "acc-lead": 0})
	if r.Summary.UnattributedPoints != 0 {
		t.Errorf("unattributed = %.2f, want 0: an equal split still credits people", r.Summary.UnattributedPoints)
	}
	f := flag(t, r, sprint.FlagWorklogSplitEqually)
	for _, want := range []string{"35h", "2 people", "no breakdown", "divided equally", "@Person 3h"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("flag message %q should mention %q", f.Message, want)
		}
	}
}

// A figure beside some names but not all is not a breakdown. Equal, and
// the flag says which kind of gap it was.
func TestPartialBreakdownDividesEquallyAndSaysSo(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-a", " 4h ", "acc-b", "") })
	expectDelivered(t, r, map[string]float64{"acc-a": 3, "acc-b": 3})
	if f := flag(t, r, sprint.FlagWorklogSplitEqually); !strings.Contains(f.Message, "only some of the names") {
		t.Errorf("flag should say the breakdown was partial, got %q", f.Message)
	}
}

// A ticket with one clean entry and one shared entry: shares add up.
func TestMixedEntriesAddUp(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) {
		workedBy(is, "acc-lead", 6, "acc-a", "")
		workedBy(is, "acc-lead", 6, "acc-a", "", "acc-b", "")
	})
	expectDelivered(t, r, map[string]float64{"acc-a": 4.5, "acc-b": 1.5})
	if got := deliveredBy(r, "acc-a") + deliveredBy(r, "acc-b"); got != 6 {
		t.Errorf("shares sum to %.2f, want the ticket's 6", got)
	}
}

// Figures that add up to the time logged, under any reading of an unwritten
// unit, need no remark. Figures that match no reading are reported, and the
// entry is still divided by them.
func TestFiguresThatDoNotAddUpAreFlagged(t *testing.T) {
	cases := map[string]struct {
		hours float64
		named []string
		flag  bool
	}{
		"hours agree":                 {4, []string{"acc-a", " 3h ", "acc-b", " 1h"}, false},
		"hours disagree":              {12, []string{"acc-a", " 3h ", "acc-b", " 1h"}, true},
		"no unit, reads as points":    {27, []string{"acc-a", " 3,5 ", "acc-b", " 1"}, false},
		"no unit, reads as hours":     {4.5, []string{"acc-a", " 3,5 ", "acc-b", " 1"}, false},
		"no unit, reads as nothing":   {35, []string{"acc-a", " 3,5 ", "acc-b", " 1"}, true},
		"within a tenth is agreement": {26, []string{"acc-a", " 3,5 ", "acc-b", " 1"}, false},
	}
	for name, c := range cases {
		r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", c.hours, c.named...) })
		if got := flagCount(r, sprint.FlagWorklogFiguresDisagree) == 1; got != c.flag {
			t.Errorf("%s: flagged = %v, want %v", name, got, c.flag)
		}
		if a, b := deliveredBy(r, "acc-a"), deliveredBy(r, "acc-b"); a+b != 6 || a <= b {
			t.Errorf("%s: shares %.2f and %.2f should still divide the 6 points by the figures", name, a, b)
		}
	}
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 35, "acc-a", " 3,5 ", "acc-b", " 1") })
	f := flag(t, r, sprint.FlagWorklogFiguresDisagree)
	for _, want := range []string{"35h logged", "add up to 4.5 with no unit", "27h as points or days", "divided by the figures"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("flag message %q should mention %q", f.Message, want)
		}
	}
}
