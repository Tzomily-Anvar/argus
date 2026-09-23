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
// comment @mentions the given accounts.
func workedBy(is *jira.Issue, author string, hours float64, mentions ...string) {
	nodes := make([]string, 0, len(mentions))
	for _, m := range mentions {
		nodes = append(nodes, `{"type":"mention","attrs":{"id":"`+m+`","text":"@Person"}}`)
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

// Under author attribution nothing changes: the lead logged it, the lead
// is credited, whoever the comment names.
func TestAuthorAttributionCreditsWhoeverLogged(t *testing.T) {
	r := attributed(t, config.AttributeToAuthor, func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-a") })
	if got := deliveredBy(r, "acc-lead"); got != 6 {
		t.Errorf("lead delivered %.2f, want 6", got)
	}
	if got := deliveredBy(r, "acc-a"); got != 0 {
		t.Errorf("person A delivered %.2f, want 0", got)
	}
}

// Under mention attribution the one person named is credited, and the
// author - who only recorded it - is not.
func TestMentionAttributionCreditsTheOnePersonNamed(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-a") })
	if got := deliveredBy(r, "acc-a"); got != 6 {
		t.Errorf("person A delivered %.2f, want 6", got)
	}
	if got := deliveredBy(r, "acc-lead"); got != 0 {
		t.Errorf("lead delivered %.2f, want 0: they recorded the work, they did not do it", got)
	}
	if r.Summary.UnattributedPoints != 0 {
		t.Errorf("unattributed = %.2f, want 0", r.Summary.UnattributedPoints)
	}
}

// Naming yourself, or naming nobody, is the author's own time.
func TestMentionAttributionFallsBackToTheAuthor(t *testing.T) {
	cases := map[string]func(*jira.Issue){
		"self":       func(is *jira.Issue) { workedBy(is, "acc-lead", 6, "acc-lead") },
		"no comment": func(is *jira.Issue) { workedBy(is, "acc-lead", 6) },
	}
	for name, log := range cases {
		r := attributed(t, config.AttributeToMention, log)
		if got := deliveredBy(r, "acc-lead"); got != 6 {
			t.Errorf("%s: lead delivered %.2f, want 6", name, got)
		}
	}
}

// One Time Spent against two names cannot be split honestly. Under mention
// attribution it is credited to nobody, reported as unattributed so the
// totals still reconcile, and flagged - and not as "unassigned", which is
// a different gap.
func TestAmbiguousEntryIsWithheldAndFlagged(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) { workedBy(is, "acc-lead", 35, "acc-a", "acc-b") })
	for _, id := range []string{"acc-lead", "acc-a", "acc-b"} {
		if got := deliveredBy(r, id); got != 0 {
			t.Errorf("%s delivered %.2f, want 0", id, got)
		}
	}
	if r.Summary.UnattributedPoints != 6 {
		t.Errorf("unattributed = %.2f, want 6", r.Summary.UnattributedPoints)
	}
	if n := flagCount(r, sprint.FlagWorklogAmbiguous); n != 1 {
		t.Fatalf("%d %s flags, want 1", n, sprint.FlagWorklogAmbiguous)
	}
	if n := flagCount(r, sprint.FlagDoneUnassigned); n != 0 {
		t.Errorf("%d %s flags, want 0: the ticket has an assignee and logged time", n, sprint.FlagDoneUnassigned)
	}
	f := flag(t, r, sprint.FlagWorklogAmbiguous)
	for _, want := range []string{"35h", "2 people", "6.00 points", "one entry per person"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("flag message %q should mention %q", f.Message, want)
		}
	}
}

// Under author attribution the same entry stays with the author, but is
// still worth a look.
func TestAmbiguousEntryUnderAuthorAttributionIsFlaggedOnly(t *testing.T) {
	r := attributed(t, config.AttributeToAuthor, func(is *jira.Issue) { workedBy(is, "acc-lead", 35, "acc-a", "acc-b") })
	if got := deliveredBy(r, "acc-lead"); got != 6 {
		t.Errorf("lead delivered %.2f, want 6", got)
	}
	if r.Summary.UnattributedPoints != 0 {
		t.Errorf("unattributed = %.2f, want 0", r.Summary.UnattributedPoints)
	}
	if n := flagCount(r, sprint.FlagWorklogAmbiguous); n != 1 {
		t.Errorf("%d %s flags, want 1", n, sprint.FlagWorklogAmbiguous)
	}
}

// A ticket with one clean entry and one ambiguous one splits by hours:
// the clean share is credited, the ambiguous share withheld.
func TestMixedEntriesSplitByHours(t *testing.T) {
	r := attributed(t, config.AttributeToMention, func(is *jira.Issue) {
		workedBy(is, "acc-lead", 6, "acc-a")
		workedBy(is, "acc-lead", 6, "acc-a", "acc-b")
	})
	if got := deliveredBy(r, "acc-a"); got != 3 {
		t.Errorf("person A delivered %.2f, want 3", got)
	}
	if r.Summary.UnattributedPoints != 3 {
		t.Errorf("unattributed = %.2f, want 3", r.Summary.UnattributedPoints)
	}
	if f := flag(t, r, sprint.FlagWorklogAmbiguous); !strings.Contains(f.Message, "3.00 points") {
		t.Errorf("flag should say what was withheld, got %q", f.Message)
	}
}
