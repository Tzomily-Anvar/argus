package page

import (
	"fmt"
	"io"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The footer naming the conventions in force. Every figure above is
// only as good as the rules it was counted under, and the rules are
// settings that can change between sprints; a page that names them can
// still be read when they have.
var countedTmpl = tmpl("counted", `<h2>{{.Heading}}</h2>
{{range .Paragraphs}}<p>{{.}}</p>
{{end}}`)

func renderCounted(w io.Writer, rep sprint.Report, in Inputs, _ map[string]bool) error {
	c := in.Conventions
	v := struct {
		Heading    string
		Paragraphs []string
	}{Heading: headings[SectionCounted]}
	add := func(s string) { v.Paragraphs = append(v.Paragraphs, s) }

	add(doneSentence(c))
	add(windowSentence(rep.Sprint))

	if len(c.Containers) == 0 {
		add("No issue type is treated as a container, so every sized ticket counts as delivery.")
	} else {
		add(fmt.Sprintf("%s are containers: their points are a rollup of the work beneath them and are never counted as delivery.",
			join(c.Containers, "and")))
	}

	switch c.AbsenceCost {
	case "share":
		add("A day off cost a share of the baseline, baseline divided by sprint length, rather than a whole point: " +
			"somebody whose few points are spread thinly across the sprint loses a thin slice of them per day away.")
	default:
		add("A day off cost a whole point of capacity, whatever the baseline: a point is a day, so a day away is a point not delivered.")
	}

	switch c.WorklogAttribution {
	case "mention":
		add("A logged entry credits the one account its comment mentions, when the author is somebody else; " +
			"otherwise it credits the author. This is for a team where one person logs the time of the whole team at close.")
	default:
		add("A logged entry credits the account that logged it.")
	}

	if c.HoursPerPoint > 0 {
		add(fmt.Sprintf("One point is taken as %s hours, which is how logged time becomes points on carryover.", num(c.HoursPerPoint)))
	}
	if c.SprintLengthDays > 0 {
		add(fmt.Sprintf("The sprint is %s long, which is what turns a day off into a share of a baseline.",
			plural(c.SprintLengthDays, "working day", "working days")))
	}
	if c.PointsField != "" {
		add("Points are read from the " + c.PointsField + " field.")
	}
	return countedTmpl.Execute(w, v)
}

// doneSentence names the statuses that counted, and where the list came
// from. Matched by name rather than by category, because teams routinely
// have several statuses in the done category and disagree about which
// mean finished.
func doneSentence(c Conventions) string {
	if len(c.DoneStatuses) == 0 {
		return "No status was named as delivered, so nothing counted as delivery."
	}
	s := "Delivered means a ticket reached "
	if len(c.DoneStatuses) == 1 {
		s += "the status " + c.DoneStatuses[0] + "."
	} else {
		s += "one of the statuses " + join(c.DoneStatuses, "or") + "."
	}
	switch c.DoneSource {
	case DoneSourceJira:
		s += " That list was read from the workflow in Jira."
	case DoneSourceSetting:
		s += " That list is a setting in Argus rather than the workflow in Jira."
	}
	return s
}

// windowSentence says which stretch of time the sprint claimed work in.
// It is not the sprint's dates: the sprint completes when somebody
// clicks Complete Sprint, routinely days after it was meant to end, and
// that click decides which sprint a finished ticket belongs to.
func windowSentence(s sprint.SprintInfo) string {
	if s.CountsUntil.IsZero() {
		return fmt.Sprintf("Work counted here if it finished on or after %s; the sprint is still open, so the window has no end yet.",
			day(s.CountsFrom))
	}
	return fmt.Sprintf("Work counted here if it finished between %s and %s: from the sprint opening to the moment it was completed in Jira, not to the date it was meant to end.",
		day(s.CountsFrom), day(s.CountsUntil))
}
