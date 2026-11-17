package page

import (
	"io"
	"math"
	"sort"
	"strconv"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The Run / Build split: delivered points by epic class - Run against
// Build, or whatever classes a team configures - with the ratio between
// them, then each epic that contributed and how much. The same figures
// as the dashboard's section of the same name, so the two agree.
var epicsTmpl = tmpl("epics", `<h2>{{.Heading}}</h2>
{{if not .Rows}}<p>Nothing was delivered under an epic this sprint.</p>
{{else}}{{if .Classes}}<table><tbody>
<tr>{{range .Classes}}<th>{{.Name}}</th>{{end}}{{if .Ratio}}<th>Ratio</th>{{end}}</tr>
<tr>{{range .Classes}}<td>{{.Points}} pts ({{.Share}})</td>{{end}}{{if .Ratio}}<td>{{.Ratio}}</td>{{end}}</tr>
</tbody></table>
{{end}}<table><tbody>
<tr><th>Epic</th>{{if .Classes}}<th>Class</th>{{end}}<th>Points</th><th>Share</th><th>Issues</th></tr>
{{range .Rows}}<tr><td>{{if .URL}}<a href="{{.URL}}">{{.Key}}</a> {{end}}{{.Name}}</td>{{if $.Classes}}<td>{{.Class}}</td>{{end}}<td>{{.Points}}</td><td>{{.Share}}</td><td>{{.Issues}}</td></tr>
{{end}}</tbody></table>
<p><em>Delivered points, credited this sprint, grouped by the epic each ticket sits under. Classes come from ARGUS_JIRA_EPIC_CLASSES, matched against the epic's name.</em></p>
{{end}}`)

type epicClassView struct{ Name, Points, Share string }

type epicView struct {
	Key, URL, Name, Class, Points, Share string
	Issues                               int
}

func renderEpics(w io.Writer, rep sprint.Report, _ Inputs, _ map[string]bool) error {
	v := struct {
		Heading string
		Classes []epicClassView
		Ratio   string
		Rows    []epicView
	}{Heading: headings[SectionEpics]}

	byClass := map[string]float64{}
	var classed bool
	for _, e := range rep.Epics {
		class := e.Class
		if class != "" {
			classed = true
		} else {
			class = "Other"
		}
		byClass[class] += e.Points
		v.Rows = append(v.Rows, epicView{Key: e.Key, URL: e.URL, Name: e.Name, Class: class,
			Points: num(e.Points), Share: pct(e.Share), Issues: e.IssueCount})
	}
	if classed {
		total := 0.0
		names := make([]string, 0, len(byClass))
		for name, pts := range byClass {
			names = append(names, name)
			total += pts
		}
		// Largest first, with Other last whatever its size: it is what
		// no class matched, not a class of its own.
		sort.Slice(names, func(i, j int) bool {
			if (names[i] == "Other") != (names[j] == "Other") {
				return names[j] == "Other"
			}
			if byClass[names[i]] != byClass[names[j]] {
				return byClass[names[i]] > byClass[names[j]]
			}
			return names[i] < names[j]
		})
		var classes []string
		for _, name := range names {
			share := 0.0
			if total > 0 {
				share = byClass[name] / total
			}
			v.Classes = append(v.Classes, epicClassView{Name: name, Points: num(byClass[name]), Share: pct(share)})
			if name != "Other" {
				classes = append(classes, name)
			}
		}
		// Two classes read as a ratio between them alone, so work under
		// no class does not shrink both sides: Build 67 : Run 33. With
		// more than two the shares beside each say it already.
		if len(classes) == 2 {
			a, b := byClass[classes[0]], byClass[classes[1]]
			if a+b > 0 {
				ra := int(math.Round(100 * a / (a + b)))
				v.Ratio = classes[0] + " " + strconv.Itoa(ra) + " : " + classes[1] + " " + strconv.Itoa(100-ra)
			}
		}
	}
	return epicsTmpl.Execute(w, v)
}
