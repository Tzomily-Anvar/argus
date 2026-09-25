package page

import (
	"fmt"
	"io"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// What refinement expected against what the work cost. The framing is
// the dashboard's: nothing here is called accuracy, the two directions
// are kept apart, and the coverage sits beside the totals because they
// speak for the compared subset and not for the sprint.
var calibrationTmpl = tmpl("calibration", `<h2>{{.Heading}}</h2>
{{if .Message}}<p>{{.Message}}</p>
{{else}}<table><tbody>
<tr><th>Estimated</th><th>Actual</th><th>Difference</th><th>Variance</th></tr>
<tr><td>{{.Estimated}}</td><td>{{.Actual}}</td><td>{{.Difference}}</td><td>{{.Variance}}</td></tr>
</tbody></table>
<p>{{.Coverage}} {{.Split}}{{if .Thin}} {{.Thin}}{{end}}</p>
{{if .Diverged}}<p>Furthest from estimate:</p>
<ul>
{{range .Diverged}}<li><a href="{{.URL}}">{{.Key}}</a> {{.Summary}} · {{.Figures}}</li>
{{end}}</ul>
{{end}}{{end}}`)

type divergenceView struct {
	Key, URL, Summary, Figures string
}

type calibrationView struct {
	Heading string

	// Message is the plain statement shown instead of figures when there
	// is nothing to compare, and why.
	Message string

	Estimated, Actual, Difference, Variance string
	Coverage, Split, Thin                   string
	Diverged                                []divergenceView
}

func renderCalibration(w io.Writer, rep sprint.Report, _ Inputs, _ map[string]bool) error {
	c := rep.Calibration
	v := calibrationView{Heading: headings[SectionCalibration]}

	switch {
	case !c.Configured:
		v.Message = "Only one size is recorded on a ticket here, so there is nothing to compare. " +
			"Point a second field at the refinement estimate and this section fills in."
	case c.Finished == 0:
		v.Message = "Nothing finished here yet, so there is nothing to compare."
	case c.Compared == 0:
		v.Message = fmt.Sprintf("None of the %s finished here carries both an estimate and an actual, "+
			"so there is nothing to compare. A ticket needs both numbers before the gap between them means anything.",
			plural(c.Finished, "ticket", "tickets"))
	}
	if v.Message != "" {
		return calibrationTmpl.Execute(w, v)
	}

	v.Estimated = num(c.Estimated)
	v.Actual = num(c.Actual)
	v.Difference = signed(c.Difference)
	v.Variance = "—"
	if c.HasVariance {
		v.Variance = signed(c.Variance*100) + "%"
	}
	v.Coverage = fmt.Sprintf("%d of %s finished here carried both figures, so these totals speak for that %s of the sprint rather than all of it.",
		c.Compared, plural(c.Finished, "ticket", "tickets"), pct(float64(c.Compared)/float64(c.Finished)))
	v.Split = fmt.Sprintf("Of those, %d matched, %d cost more than estimated and %d cost less.", c.Matched, c.Over, c.Under)
	if c.Thin {
		v.Thin = "Fewer than ten were compared, so this is a reading on a few tickets rather than on refinement."
	}
	for _, d := range c.Diverged {
		v.Diverged = append(v.Diverged, divergenceView{
			Key:     d.Key,
			URL:     d.URL,
			Summary: d.Summary,
			Figures: num(d.Estimated) + " → " + num(d.Actual) + " (" + signed(d.Difference) + ")",
		})
	}
	return calibrationTmpl.Execute(w, v)
}
