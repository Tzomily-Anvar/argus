package page

import (
	"io"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The three lists of tickets: containers that concluded, the flags, and
// what was still open at close. Each key links to its ticket, because
// trust in a report comes from being able to check it.

// Stories concluded, with the work beneath each beside its own figure.
// The two disagreeing is the interesting case, and it reads better in a
// table than as a flag nobody opens.
var storiesTmpl = tmpl("stories", `<h2>{{.Heading}}</h2>
{{if not .Rows}}<p>None finished this sprint.</p>
{{else}}<table><tbody>
<tr><th>Story</th><th>Summary</th><th>Points</th><th>Linked work</th></tr>
{{range .Rows}}<tr><td><a href="{{.URL}}">{{.Key}}</a></td><td>{{.Summary}}</td><td>{{.Points}}</td><td>{{.Linked}}</td></tr>
{{end}}</tbody></table>
<p><em>Finished here means the Story is done and so is everything linked beneath it. Their points are a rollup of that work, so they are reported here and never counted as delivery.</em></p>
{{end}}`)

type storyView struct {
	Key, URL, Summary, Points, Linked string
}

func renderStories(w io.Writer, rep sprint.Report, _ Inputs, _ map[string]bool) error {
	v := struct {
		Heading string
		Rows    []storyView
	}{Heading: headings[SectionStories]}
	for _, st := range rep.Stories {
		row := storyView{Key: st.Key, URL: st.URL, Summary: st.Summary, Points: num(st.Points), Linked: "no linked work"}
		if st.LinkedCount > 0 {
			row.Linked = plural(st.LinkedCount, "item", "items") + " · " + num(st.LinkedPoints) + " pts"
		}
		v.Rows = append(v.Rows, row)
	}
	return storiesTmpl.Execute(w, v)
}

// The flags, one item each: the kind as a label, the message, then the
// ticket or tickets behind it. A flag speaking for several keeps them
// on one line, because nine lines is how a list stops being read.
var flagsTmpl = tmpl("flags", `<h2>{{.Heading}}</h2>
{{if not .Rows}}<p>Nothing needs a second look.</p>
{{else}}<ul>
{{range .Rows}}<li><strong>{{.Kind}}</strong> {{.Message}}{{if .URL}} <a href="{{.URL}}">{{.Key}}</a>{{else if .Key}} <code>{{.Key}}</code>{{end}}{{if .Keys}} <code>{{.Keys}}</code>{{end}}{{if .JQL}} <a href="{{.JQL}}">see all in Jira</a>{{end}}</li>
{{end}}</ul>
{{end}}`)

type flagView struct {
	Kind, Message, Key, URL, Keys, JQL string
}

func renderFlags(w io.Writer, rep sprint.Report, _ Inputs, _ map[string]bool) error {
	v := struct {
		Heading string
		Rows    []flagView
	}{Heading: headings[SectionFlags]}
	for _, f := range rep.Flags {
		v.Rows = append(v.Rows, flagView{
			// The kind reads as words, the way the dashboard's badge
			// shows it, rather than as the identifier it is in code.
			Kind:    strings.ReplaceAll(f.Kind, "_", " "),
			Message: f.Message,
			Key:     f.Key,
			URL:     f.URL,
			Keys:    strings.Join(f.Keys, " "),
			JQL:     f.JQL,
		})
	}
	return flagsTmpl.Execute(w, v)
}

// Carryover: still open when the sprint closed. Work nobody picked up is
// marked, because it is not the same as work somebody could not finish,
// and work somebody did pick up without logging time is marked too,
// since that is the entry the next sprint has to make up.
var carryoverTmpl = tmpl("carryover", `<h2>{{.Heading}}</h2>
{{if not .Rows}}<p>Nothing carried over.</p>
{{else}}<table><tbody>
<tr><th>Ticket</th><th>Summary</th><th>Status</th><th>Note</th></tr>
{{range .Rows}}<tr><td><a href="{{.URL}}">{{.Key}}</a></td><td>{{.Summary}}</td><td>{{.Status}}</td><td>{{.Note}}</td></tr>
{{end}}</tbody></table>
{{end}}`)

type carryView struct {
	Key, URL, Summary, Status, Note string
}

func renderCarryover(w io.Writer, rep sprint.Report, _ Inputs, _ map[string]bool) error {
	v := struct {
		Heading string
		Rows    []carryView
	}{Heading: headings[SectionCarryover]}
	for _, r := range rep.Carry {
		row := carryView{Key: r.Key, URL: r.URL, Summary: r.Summary, Status: r.Status}
		switch {
		case !r.Active:
			row.Note = "never started"
		case !r.TimeLogged:
			row.Note = "no time logged"
		}
		v.Rows = append(v.Rows, row)
	}
	return carryoverTmpl.Execute(w, v)
}
