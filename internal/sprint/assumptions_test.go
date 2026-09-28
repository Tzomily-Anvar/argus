package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// Each row of the assumption panel, from a table: the report and the
// declared values that make it fire, and one that does not. Assumptions
// are the second class of flag and never mixed with the first, so the
// table also holds that Flags stays untouched.

func status(id, name, cat string) jira.Status {
	s := jira.Status{ID: id, Name: name}
	s.Category.Key = cat
	return s
}

// baseline is a closed sprint with one finished, sized Task and no
// containers configured, on which nothing fires.
func baseline(t *testing.T) (sprint.Report, sprint.Inputs) {
	t.Helper()
	in := inputs(t)
	in.Rules.Container = nil
	in.BaseURL = "https://jira.example"
	in.Declared = sprint.Declared{
		ReadAt: time.Now().Add(-time.Hour),
		Statuses: []jira.Status{
			status("1", "To Do", "new"), status("2", "In Progress", "indeterminate"), status("3", "Done", "done"),
		},
		DoneFromJira: true,
		Columns: []jira.BoardColumn{
			{Name: "To Do", StatusIDs: []string{"1"}}, {Name: "In Progress", StatusIDs: []string{"2"}},
			{Name: "Done", StatusIDs: []string{"3"}},
		},
		Estimation: "field", BoardField: pointsField, BoardFieldName: "Story Points",
		NamedField: pointsField, PointsFromJira: true,
		SprintLengthDays: 10,
	}
	return sprint.Build(in), in
}

func kinds(flags []sprint.Flag) []string {
	var out []string
	for _, f := range flags {
		out = append(out, f.Kind)
	}
	return out
}

func TestAssumptionRows(t *testing.T) {
	cases := []struct {
		name  string
		setup func(rep *sprint.Report, in *sprint.Inputs)
		want  string // the kind that fires, "" for none
		text  string // something the message has to say
	}{
		{"nothing to check on a tidy board", func(*sprint.Report, *sprint.Inputs) {}, "", ""},
		{"no container reached by any child", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Rules.Container = []string{"Story"}
			rep.Summary.Containers, rep.Summary.ContainersUnlinked = 12, 12
			rep.Summary.IssuesByType["Story"] = 12
		}, sprint.AssumeContainersUnreached, "any of the 12 containers"},
		{"some containers reached by nothing", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Rules.Container = []string{"Story"}
			rep.Summary.Containers, rep.Summary.ContainersUnlinked = 84, 7
			rep.Summary.IssuesByType["Story"] = 84
		}, sprint.AssumeContainersUnlinked, "7 of 84 containers"},
		{"an abandonment-named status nothing finished in is not questioned", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.Statuses = append(in.Declared.Statuses, status("4", "Superseded", "done"))
		}, "", ""},
		{"a done-category status that suggests abandonment", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.Statuses = append(in.Declared.Statuses, status("4", "Superseded", "done"))
			rep.Summary.DoneByStatus["Superseded"] = 9
		}, sprint.AssumeDoneMayNotMean, `9 issues finished in "Superseded"`},
		{"an abandonment name is not questioned when the override names it", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.Statuses = append(in.Declared.Statuses, status("4", "Cancelled", "done"))
			in.Declared.DoneFromJira = false
			in.Rules.Done = []string{"Done", "Cancelled"}
		}, "", ""},
		{"a board column that disagrees with a category", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.Statuses = append(in.Declared.Statuses, status("5", "Ready for production", "done"))
			in.Declared.Columns[1].StatusIDs = []string{"2", "5"}
		}, sprint.AssumeColumnDisagrees, `"Ready for production" is in Jira's done category, and your board has it in the "In Progress" column`},
		{"an override naming a status that does not exist", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.DoneFromJira = false
			in.Rules.Done = []string{"Done", "Complete"}
		}, sprint.AssumeOverrideUnknown, `names "Complete"`},
		{"issues finished with nothing in the board's field", func(rep *sprint.Report, in *sprint.Inputs) {
			rep.Summary.DoneUnsized = 23
		}, sprint.AssumeFinishedUnsized, "23 issues finished with nothing in Story Points"},
		{"a configured container type with no issues", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Rules.Container = []string{"Story"}
		}, sprint.AssumeContainerTypeAbsent, `"Story" is set as a container and this sprint contains none`},
		{"an epic-level container is never in a sprint, so its absence says nothing", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Rules.Container = []string{"Epic"}
			in.Rules.ExcludedFromSayDo = []string{"Epic"}
		}, "", ""},
		{"declared sprint length differing from the setting", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.SprintLengthDays, in.Declared.SprintLengthSetting = 15, 10
		}, sprint.AssumeSprintLength, "run 15 working days; ARGUS_JIRA_SPRINT_LENGTH_DAYS says 10"},
		{"a day's difference in sprint length is not worth a row", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.SprintLengthDays, in.Declared.SprintLengthSetting = 11, 10
		}, "", ""},
		{"worklog coverage low with carryover present", func(rep *sprint.Report, in *sprint.Inputs) {
			rep.Summary.CarriedActive = 3
			rep.Carry = []sprint.Row{{Key: "ABC-2", Active: true}, {Key: "ABC-3", Active: true}, {Key: "ABC-4", Active: true, TimeLogged: true}}
		}, sprint.AssumeWorklogCoverage, "Most carried work has no logged time (2 of 3 tickets)"},
		{"declared values stale", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.Stale = true
			in.Declared.ReadAt = rep.GeneratedAt.Add(-9 * 24 * time.Hour)
		}, sprint.AssumeStale, "last read from Jira 9 days ago"},
		{"the board's field held back where the name lookup disagrees", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.BoardField, in.Declared.NamedField, in.Declared.PointsFromJira = "customfield_10026", pointsField, false
			in.Declared.BoardFieldPopulated, in.Declared.NamedFieldPopulated = 40, 38
		}, sprint.AssumePointsFieldDisagrees, "Set ARGUS_JIRA_POINTS_FIELD=customfield_10026 to accept"},
		{"a board estimating by issue count", func(rep *sprint.Report, in *sprint.Inputs) {
			in.Declared.Estimation, in.Declared.BoardField, in.Declared.PointsFromJira = "issueCount", "", false
		}, sprint.AssumeIssueCountBoard, "estimates by issue count"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep, in := baseline(t)
			before := len(rep.Flags)
			c.setup(&rep, &in)
			got := sprint.Assumptions(rep, in)

			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("assumptions = %v, want none", kinds(got))
				}
				return
			}
			if len(got) != 1 || got[0].Kind != c.want {
				t.Fatalf("assumptions = %v, want exactly %s", kinds(got), c.want)
			}
			if !strings.Contains(got[0].Message, c.text) {
				t.Errorf("message = %q, want it to say %q", got[0].Message, c.text)
			}
			if !strings.Contains(got[0].Message, "ARGUS_") && !strings.Contains(got[0].Message, "`argus") {
				t.Errorf("message = %q, want it to end in a setting or a command", got[0].Message)
			}
			if len(rep.Flags) != before {
				t.Errorf("Flags changed from %d to %d; the two classes must never mix", before, len(rep.Flags))
			}
		})
	}
}

// Every assumption whose claim can be checked in Jira carries a link
// that checks it, and none carries a link to nowhere.
func TestAssumptionsCarryJQLWhereCheckable(t *testing.T) {
	rep, in := baseline(t)
	in.Rules.Container = []string{"Story"}
	in.Declared.Statuses = append(in.Declared.Statuses, status("4", "Superseded", "done"))
	rep.Summary.DoneUnsized = 2
	for _, f := range sprint.Assumptions(rep, in) {
		if f.JQL == "" {
			t.Errorf("%s carries no JQL link", f.Kind)
		} else if !strings.HasPrefix(f.JQL, in.BaseURL+"/issues/?jql=") {
			t.Errorf("%s links to %q, want a search on the site", f.Kind, f.JQL)
		}
	}
}

// Build fills the panel itself, so a report never crosses to the browser
// with the field missing, and the counts it fills come from the issues.
func TestBuildFillsTheAssumptionPanel(t *testing.T) {
	tidy := inputs(t)
	tidy.Rules.Container = nil
	if got := sprint.Build(tidy).Assumptions; got == nil || len(got) != 0 {
		t.Fatalf("a tidy board's assumptions = %v, want an empty list rather than null", got)
	}

	in := inputs(t)
	in.Issues = append(in.Issues, issue(t, "ABC-9", "Story", "Done", "acc-a", 8))
	rep := sprint.Build(in)
	if rep.Assumptions == nil {
		t.Fatal("assumptions is nil; it must be a list, empty or not")
	}
	if rep.Summary.Containers != 1 || rep.Summary.ContainersUnlinked != 1 || rep.Summary.IssuesByType["Story"] != 1 {
		t.Errorf("summary counts = %d containers, %d unlinked, %v by type", rep.Summary.Containers,
			rep.Summary.ContainersUnlinked, rep.Summary.IssuesByType)
	}
	if got := kinds(rep.Assumptions); len(got) != 1 || got[0] != sprint.AssumeContainersUnreached {
		t.Errorf("assumptions = %v, want the one unreached container", got)
	}
}
