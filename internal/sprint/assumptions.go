package sprint

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// The assumption panel: the places Argus may be reading this board
// wrong, said in the report at the moment the number is wrong. A
// documentation page is read by people who already suspect something.
//
// Every entry here compares something Jira declares against what the
// report did with it, or names a count that only makes sense if a
// convention holds. None of them changes a figure: observation may
// inform a person, and it never sets a value.

// Declared is what the team's Jira configuration said when the report
// was built, dated. The service fills it from the reads in the jira
// package and keeps the last successful reading when one fails; Build
// only ever reads it.
type Declared struct {
	// ReadAt is when the reads last succeeded, and Stale whether the
	// latest attempt failed, so these are the previous reading.
	ReadAt time.Time
	Stale  bool

	// Statuses are the project's, each with the category it declares,
	// and DoneFromJira whether the delivered set in force is the done
	// category rather than an override.
	Statuses     []jira.Status
	DoneFromJira bool

	// BoardID is the board these came from, and Columns its own
	// arrangement of statuses.
	BoardID int64
	Columns []jira.BoardColumn

	// The board's estimation: its type, field or issueCount, and the
	// field it names. NamedField is what the setting or the name lookup
	// gave instead, and PointsFromJira whether the board's field is the
	// one in force. The two populated counts, over this sprint's issues,
	// are what a person weighs when the fields disagree.
	Estimation          string
	BoardField          string
	BoardFieldName      string
	NamedField          string
	PointsFromJira      bool
	BoardFieldPopulated int
	NamedFieldPopulated int

	// SprintLengthDays is the working days the last closed sprint on
	// this board ran, zero when there is no board, and
	// SprintLengthSetting the override, zero when unset.
	SprintLengthDays    int
	SprintLengthSetting int
}

// Assumption kinds.
const (
	AssumeContainersUnreached  = "containers_unreached"
	AssumeContainersUnlinked   = "containers_unlinked"
	AssumeDoneMayNotMean       = "done_status_questionable"
	AssumeColumnDisagrees      = "column_disagrees"
	AssumeOverrideUnknown      = "override_unknown_status"
	AssumeFinishedUnsized      = "finished_unsized"
	AssumeContainerTypeAbsent  = "container_type_absent"
	AssumeSprintLength         = "sprint_length"
	AssumeWorklogCoverage      = "worklog_coverage"
	AssumeStale                = "declared_stale"
	AssumePointsFieldDisagrees = "points_field_disagrees"
	AssumeIssueCountBoard      = "issue_count_board"
)

// abandonment are the words in a status name that suggest work was
// dropped rather than delivered. A heuristic about English and nothing
// more: it reads the declared name of a status, never how many issues
// passed through it, and it only ever opens a question. It never
// excludes a status by itself.
var abandonment = []string{"cancel", "reject", "won't do", "wont do", "duplicate", "obsolete", "supersed", "superfluous"}

func suggestsAbandonment(status string) bool {
	name := strings.ToLower(status)
	for _, w := range abandonment {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// Assumptions lists what a person should check about how this board was
// read. Pure: it looks at the report and what Jira declared and nothing
// else, so every row can be tested from a table.
func Assumptions(rep Report, in Inputs) []Flag {
	// A list even when empty: this crosses to a browser as JSON, and a
	// null where an array was promised is a crash rather than a clean
	// panel - which, for this panel, is the normal case.
	out := []Flag{}
	add := func(f Flag) { out = append(out, f) }
	d := in.Declared
	sum := rep.Summary
	sprintID := rep.Sprint.JiraID

	// Containers nothing reaches. All of them says Argus is looking in
	// the wrong place; some of them is a finding about the data.
	if sum.Containers > 0 && sum.ContainersUnlinked == sum.Containers {
		add(Flag{Kind: AssumeContainersUnreached,
			Message: fmt.Sprintf("No work is linked beneath any of the %s in this sprint. Argus looks for it through the link types %s; on this board the association may be somewhere else. Check ARGUS_JIRA_STORY_LINK_TYPES and ARGUS_JIRA_CONTAINER_TYPES",
				plural(sum.Containers, "container"), strings.Join(in.StoryLinkTypes, ", ")),
			JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND issuetype IN (%s)`, sprintID, quoteList(in.Rules.Container)))})
	} else if sum.ContainersUnlinked > 0 {
		add(Flag{Kind: AssumeContainersUnlinked,
			Message: fmt.Sprintf("%d of %d containers have no linked work at all, so nothing rolls up to them. If the type is not a container for you, change ARGUS_JIRA_CONTAINER_TYPES",
				sum.ContainersUnlinked, sum.Containers),
			JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND issuetype IN (%s)`, sprintID, quoteList(in.Rules.Container)))})
	}

	// A done-category status whose name says the work was dropped.
	if d.DoneFromJira {
		for _, s := range d.Statuses {
			if s.Category.Key != "done" || !suggestsAbandonment(s.Name) {
				continue
			}
			add(Flag{Kind: AssumeDoneMayNotMean,
				Message: fmt.Sprintf("%s finished in %q, which declares Jira's done category and is therefore counted as delivered. Set ARGUS_JIRA_DONE_STATUSES if that is not delivery for you",
					plural(sum.DoneByStatus[s.Name], "issue"), s.Name),
				JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND status = %q`, sprintID, s.Name))})
		}
	}

	out = append(out, columnDisagreements(d, in)...)

	// An override naming a status that is not there.
	if !d.DoneFromJira && len(d.Statuses) > 0 {
		for _, name := range in.Rules.Done {
			if !statusExists(d.Statuses, name) {
				add(Flag{Kind: AssumeOverrideUnknown,
					Message: fmt.Sprintf("ARGUS_JIRA_DONE_STATUSES names %q, which is not a status this project declares. Did the workflow get renamed? Check ARGUS_JIRA_DONE_STATUSES", name)})
			}
		}
	}

	if sum.DoneUnsized > 0 {
		field := in.PointsField
		if d.PointsFromJira && d.BoardFieldName != "" {
			field = d.BoardFieldName
		}
		add(Flag{Kind: AssumeFinishedUnsized,
			Message: fmt.Sprintf("%s finished with nothing in %s and count as zero. If your team sizes in another field, set ARGUS_JIRA_POINTS_FIELD",
				plural(sum.DoneUnsized, "issue"), field),
			JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND issuetype NOT IN (%s) AND %s IS EMPTY AND status IN (%s)`,
				sprintID, quoteList(in.Rules.Container), jqlField(in.PointsField), quoteList(in.Rules.Done)))})
	}

	if sum.IssueCount+sum.Containers > 0 {
		for _, typ := range in.Rules.Container {
			if countType(sum.IssuesByType, typ) == 0 {
				add(Flag{Kind: AssumeContainerTypeAbsent,
					Message: fmt.Sprintf("%q is set as a container and this sprint contains none. If your rollups are epic-level, delivery is being counted twice. Check ARGUS_JIRA_CONTAINER_TYPES", typ),
					JQL:     jqlLink(in, fmt.Sprintf(`sprint = %d AND issuetype = %q`, sprintID, typ))})
			}
		}
	}

	if d.SprintLengthDays > 0 && d.SprintLengthSetting > 0 && abs(d.SprintLengthDays-d.SprintLengthSetting) > 1 {
		add(Flag{Kind: AssumeSprintLength,
			Message: fmt.Sprintf("Sprints on this board run %d working days; ARGUS_JIRA_SPRINT_LENGTH_DAYS says %d",
				d.SprintLengthDays, d.SprintLengthSetting)})
	}

	if f, ok := worklogCoverage(rep, in); ok {
		add(f)
	}

	if d.Stale && !d.ReadAt.IsZero() {
		add(Flag{Kind: AssumeStale,
			Message: fmt.Sprintf("Conventions were last read from Jira %s ago and could not be read for this report, which is built on that reading. `argus doctor` checks the Jira connection",
				age(rep.GeneratedAt.Sub(d.ReadAt)))})
	}

	// The points field: held back where the board and the name lookup
	// disagree, because every figure moves with it.
	if d.BoardField != "" && d.NamedField != "" && d.BoardField != d.NamedField && !d.PointsFromJira {
		add(Flag{Kind: AssumePointsFieldDisagrees,
			Message: fmt.Sprintf("The board estimates in %s (%s), which %s this sprint carry; points are read from %s, which %s carry, because that is what this install resolved by name. Set ARGUS_JIRA_POINTS_FIELD=%s to accept the board's field",
				d.BoardField, d.BoardFieldName, plural(d.BoardFieldPopulated, "issue"),
				d.NamedField, plural(d.NamedFieldPopulated, "issue"), d.BoardField),
			JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND %s IS NOT EMPTY`, sprintID, jqlField(d.BoardField)))})
	}
	if d.Estimation == "issueCount" {
		add(Flag{Kind: AssumeIssueCountBoard,
			Message: fmt.Sprintf("This board estimates by issue count, so it declares no points field; points are read from the fallback name only (%s). Set ARGUS_JIRA_POINTS_FIELD to pin one",
				nameOr(in.PointsField, "none resolved"))})
	}
	return out
}

// columnDisagreements names each status whose board column puts it
// beside statuses of the other kind - delivered beside not delivered.
// Two declared sources disagreeing is for a person to see, not for a
// tool to decide; the categories are used and the disagreement said.
func columnDisagreements(d Declared, in Inputs) []Flag {
	byID := map[string]jira.Status{}
	for _, s := range d.Statuses {
		byID[s.ID] = s
	}
	var out []Flag
	for _, col := range d.Columns {
		var done, other []jira.Status
		for _, id := range col.StatusIDs {
			s, ok := byID[id]
			if !ok {
				continue
			}
			if s.Category.Key == "done" {
				done = append(done, s)
			} else {
				other = append(other, s)
			}
		}
		if len(done) == 0 || len(other) == 0 {
			continue
		}
		// The minority is the odd one out; on a tie the done statuses
		// are named, since delivery is the figure at stake.
		odd, kind := done, "in"
		if len(done) > len(other) {
			odd, kind = other, "not in"
		}
		for _, s := range odd {
			counted := "counts it as delivered"
			if !in.Rules.IsDone(s.Name) {
				counted = "does not count it as delivered"
			}
			out = append(out, Flag{Kind: AssumeColumnDisagrees,
				Message: fmt.Sprintf("%q is %s Jira's done category, and your board has it in the %q column beside statuses that are not. Argus %s. If that is wrong, set ARGUS_JIRA_DONE_STATUSES",
					s.Name, kind, col.Name, counted),
				JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND status = %q`, in.Sprint.ID, s.Name))})
		}
	}
	return out
}

// worklogCoverage says when most carried work has no logged time, so
// its split across sprints is an even guess rather than a measurement.
// Only for a closed sprint: while one is running nobody is late yet.
func worklogCoverage(rep Report, in Inputs) (Flag, bool) {
	if !strings.EqualFold(rep.Sprint.State, "closed") || rep.Summary.CarriedActive == 0 {
		return Flag{}, false
	}
	unlogged := 0
	for _, r := range rep.Carry {
		if r.Active && !r.TimeLogged {
			unlogged++
		}
	}
	if unlogged*2 <= rep.Summary.CarriedActive {
		return Flag{}, false
	}
	return Flag{Kind: AssumeWorklogCoverage,
		Message: fmt.Sprintf("Most carried work has no logged time (%d of %d tickets), so its points are divided equally across the sprints it was active in rather than by effort. The close-out can log it once ARGUS_SPRINT_ALLOW_WRITES is on",
			unlogged, rep.Summary.CarriedActive),
		JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND timespent IS EMPTY AND status NOT IN (%s)`,
			rep.Sprint.JiraID, quoteList(in.Rules.Done))),
	}, true
}

func statusExists(statuses []jira.Status, name string) bool {
	for _, s := range statuses {
		if strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func countType(byType map[string]int, typ string) int {
	n := 0
	for name, c := range byType {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(typ)) {
			n += c
		}
	}
	return n
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func nameOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// age is a duration the way a person would say it.
func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
