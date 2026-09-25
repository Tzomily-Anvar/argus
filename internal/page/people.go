package page

import (
	"io"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The per-person table, the one part of the page that names colleagues
// against a number. Measured people first, then anybody who delivered
// without a baseline to measure against, with dashes where a figure
// would mean nothing. The Reason column is a switch of its own because
// the note behind it has never left the machine before.
var peopleTmpl = tmpl("people", `<h2>{{.Heading}}</h2>
{{if not .Rows}}<p>Nothing was delivered by anybody this sprint.</p>
{{else}}<table><tbody>
<tr><th>Person</th><th>Baseline</th><th>Capacity</th><th>Delivered</th><th>Δ</th>{{if .Reasons}}<th>Reason</th>{{end}}</tr>
{{range .Rows}}<tr><td>{{.Name}}{{if .Tag}} <em>({{.Tag}})</em>{{end}}</td><td>{{.Baseline}}</td><td>{{.Capacity}}{{if .Leave}}<br/><em>{{.Leave}}</em>{{end}}</td><td>{{.Delivered}}</td><td>{{.Delta}}</td>{{if $.Reasons}}<td>{{.Reason}}</td>{{end}}</tr>
{{end}}<tr><td><strong>Total</strong></td><td><strong>{{.Total.Baseline}}</strong></td><td><strong>{{.Total.Capacity}}</strong>{{if .Total.Leave}}<br/><em>{{.Total.Leave}}</em>{{end}}</td><td><strong>{{.Total.Delivered}}</strong></td><td><strong>{{.Total.Delta}}</strong></td>{{if .Reasons}}<td></td>{{end}}</tr>
</tbody></table>
<p><em>Container types are excluded, so this is the work itself rather than the rollups above it. Capacity is the baseline less planned leave; unplanned absence is shown beneath it and not deducted, so a shortfall it explains stays visible. A dash means the person has no baseline to measure against; their delivered points still count.</em></p>
{{end}}`)

type personView struct {
	Name, Tag                            string
	Baseline, Capacity, Delivered, Delta string

	// Leave sits beneath the capacity: what planned leave took off the
	// baseline, and what unplanned absence there was that was not taken
	// off. One place, so the delta stays a plain number.
	Leave  string
	Reason string
}

type peopleView struct {
	Heading string
	Reasons bool
	Rows    []personView
	Total   personView
}

func renderPeople(w io.Writer, rep sprint.Report, in Inputs, on map[string]bool) error {
	v := peopleView{Heading: headings[SectionPeople], Reasons: on[SectionReasons]}

	// Measured first, in the report's order, then the rest in theirs.
	// Two passes rather than a sort, so the order stays the report's
	// within each group.
	var measured, others []sprint.Person
	for _, p := range rep.People {
		if p.Measured {
			measured = append(measured, p)
		} else {
			others = append(others, p)
		}
	}

	var t struct {
		baseline, capacity, delivered, delta, shortfall, planned, unplanned float64
	}
	for _, p := range append(measured, others...) {
		row := personView{
			Name:      p.Name,
			Delivered: num(p.Delivered),
			Baseline:  "—",
			Capacity:  "—",
			Delta:     "—",
			Reason:    in.Notes[p.AccountID],
		}
		// Two different things, and conflating them is what made this
		// column confusing on the dashboard. Somebody nobody has
		// registered is a gap to close; somebody left off the roster on
		// purpose is an answer already given.
		switch {
		case !p.OnRoster:
			row.Tag = "not on roster"
		case !p.Measured:
			row.Tag = "not measured"
		}
		if p.Measured {
			row.Baseline = num(p.Baseline)
			row.Capacity = num(p.Capacity)
			row.Delta = signed(p.Delta)
			row.Leave = leave(p.PlannedDaysOff, p.UnplannedDaysOff)
			t.baseline += p.Baseline
			t.capacity += p.Capacity
			t.delta += p.Delta
			t.shortfall += p.ShortfallFromAbsence
		}
		t.delivered += p.Delivered
		t.planned += p.PlannedDaysOff
		t.unplanned += p.UnplannedDaysOff
		v.Rows = append(v.Rows, row)
	}

	// The total is the columns added up, not the summary's figures: it
	// has to agree with the rows above it, and the summary says
	// separately where delivered points went that no row holds.
	v.Total = personView{
		Baseline:  num(t.baseline),
		Capacity:  num(t.capacity),
		Delivered: num(t.delivered),
		Delta:     signed(t.delta),
		Leave:     leave(t.planned, t.unplanned),
	}
	return peopleTmpl.Execute(w, v)
}

// leave is the line beneath a capacity figure: the planned days that
// were taken off, and the unplanned days that were not.
func leave(planned, unplanned float64) string {
	var parts []string
	if planned > 0 {
		parts = append(parts, num(planned)+" planned off")
	}
	if unplanned > 0 {
		parts = append(parts, num(unplanned)+" unplanned, not deducted")
	}
	return strings.Join(parts, " · ")
}
