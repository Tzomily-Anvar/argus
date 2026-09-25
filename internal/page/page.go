// Package page renders a sprint report as Confluence storage format, the
// XHTML dialect a page body is kept in.
//
// What it renders is the block Argus owns on a sprint's page, bounded by
// two markers. Everything outside them is the reader's prose, and the
// one property that matters most is that a re-publish never touches it.
// The functions here are pure: a Report and some choices in, a string
// out, nothing read and nothing written.
//
// Every value reaches the page through html/template, so a ticket
// summary holding "<" is text on the page and never markup. Storage
// format accepts ordinary tables and lists, which is what nearly
// everything here is; the only Confluence elements used are the status
// macro for the capacity badge and the info macro for the line saying
// the block is generated.
package page

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// Sections, in the fixed order they appear on the page. The order is
// the page's rather than the caller's: a publish that names flags before
// summary still gets summary first, so two publishes with the same
// switches produce the same page.
const (
	SectionSummary     = "summary"
	SectionSayDo       = "saydo"
	SectionPeople      = "people"
	SectionReasons     = "reasons" // the Reason column on the people table; only meaningful with people
	SectionCalibration = "calibration"
	SectionStories     = "stories"
	SectionFlags       = "flags"
	SectionCarryover   = "carryover"
	SectionCounted     = "counted" // the "How this was counted" footer
)

// All is every section, in page order.
var All = []string{
	SectionSummary, SectionSayDo, SectionPeople, SectionReasons, SectionCalibration,
	SectionStories, SectionFlags, SectionCarryover, SectionCounted,
}

// headings is what each section is titled on the page. The same words
// as the dashboard's sections, so a reader moving between the two finds
// the same thing under the same name. Reasons has none: it is a column
// on the people table, not a section of its own.
var headings = map[string]string{
	SectionSummary:     "Summary",
	SectionSayDo:       "Say/do",
	SectionPeople:      "Per person",
	SectionCalibration: "Estimate against actual",
	SectionStories:     "Stories concluded",
	SectionFlags:       "Needs a look",
	SectionCarryover:   "Carryover",
	SectionCounted:     "How this was counted",
}

// Labels is a short label per section, for a checkbox. A fresh map each
// call, so a caller can edit its copy without editing everyone's.
func Labels() map[string]string {
	out := make(map[string]string, len(All))
	for id, h := range headings {
		out[id] = h
	}
	out[SectionReasons] = "Reason column"
	return out
}

// Markers bound the block Argus owns on the page. Everything outside
// them is the reader's prose and survives a re-publish.
//
// They are anchor macros, not HTML comments: Confluence drops comments
// when it saves a page, which is how the first published page lost its
// markers and grew a second block at the next publish. An anchor is
// invisible on the page and kept in storage. Confluence rewrites the
// macro on save - attributes appear, order changes - so a marker is
// found by its anchor name through the patterns below, never by the
// exact text Argus wrote.
const (
	MarkerStart = `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">argus-report-start</ac:parameter></ac:structured-macro>`
	MarkerEnd   = `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">argus-report-end</ac:parameter></ac:structured-macro>`

	MarkerStartPattern = `<ac:structured-macro\b[^>]*\bac:name="anchor"[^>]*>\s*<ac:parameter\s+ac:name="">\s*argus-report-start\s*</ac:parameter>\s*</ac:structured-macro>`
	MarkerEndPattern   = `<ac:structured-macro\b[^>]*\bac:name="anchor"[^>]*>\s*<ac:parameter\s+ac:name="">\s*argus-report-end\s*</ac:parameter>\s*</ac:structured-macro>`
)

var (
	markerStart = regexp.MustCompile(MarkerStartPattern)
	markerEnd   = regexp.MustCompile(MarkerEndPattern)
)

// Bounds finds the block on a page: the offset where the start marker
// begins and the offset just past the end marker. Each marker must be
// there exactly once and in order; anything else is refused rather than
// guessed at, because a wrong guess eats somebody's prose.
func Bounds(body string) (from, to int, err error) {
	starts, ends := markerStart.FindAllStringIndex(body, -1), markerEnd.FindAllStringIndex(body, -1)
	if len(starts) != 1 || len(ends) != 1 {
		return 0, 0, fmt.Errorf("the page holds the start marker %d times and the end marker %d times, expected once each", len(starts), len(ends))
	}
	if ends[0][0] < starts[0][1] {
		return 0, 0, errors.New("the page's end marker comes before its start")
	}
	return starts[0][0], ends[0][1], nil
}

// HasMarkers reports whether either marker is on the page at all, which
// is the question before Bounds: a page with neither gets the block
// appended, a page with one is broken.
func HasMarkers(body string) bool {
	return markerStart.MatchString(body) || markerEnd.MatchString(body)
}

// Marked checks a body about to be sent carries both markers, once, in
// order - the block itself, or a page holding it.
func Marked(body string) error {
	_, _, err := Bounds(body)
	return err
}

// Inner is what sits between the markers, with the surrounding line
// breaks dropped. Empty when there are none.
func Inner(body string) string {
	starts, ends := markerStart.FindStringIndex(body), markerEnd.FindStringIndex(body)
	if starts == nil || ends == nil || ends[0] < starts[1] {
		return ""
	}
	return strings.Trim(body[starts[1]:ends[0]], "\n")
}

// The old tool's markers, recognised on read and replaced once, so the
// pages it kept migrate without a second block appearing under the
// first.
const (
	LegacyStart = "<!-- SPRINT-REPORT:AUTO:START -->"
	LegacyEnd   = "<!-- SPRINT-REPORT:AUTO:END -->"
)

// Where the list of done statuses came from, for Conventions.DoneSource.
// The footer says which, because a list read from the workflow and a
// list typed into a setting drift apart in different ways.
const (
	DoneSourceJira    = "jira"
	DoneSourceSetting = "setting"
)

// Conventions are the rules in force when the report was built, named
// in the footer so a figure can be checked against how it was counted.
type Conventions struct {
	DoneStatuses       []string // the statuses that counted as delivered
	DoneSource         string   // DoneSourceJira or DoneSourceSetting
	Containers         []string // issue types whose points are a rollup, never delivery
	AbsenceCost        string   // point | share
	WorklogAttribution string   // author | mention
	HoursPerPoint      float64
	SprintLengthDays   int
	PointsField        string // the field's display name
}

// Inputs are the choices for one rendering.
type Inputs struct {
	Sections    []string          // which to render, in any order; rendered in the fixed order
	Notes       map[string]string // account id -> the Reason text
	Conventions Conventions
	GeneratedAt time.Time
}

// generatedNote is the line at the top of the block saying what it is.
// It sits inside the markers on purpose: a reader who deletes it has
// deleted nothing that will not be back next publish.
const generatedNote = `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>This block is generated by Argus and replaced whole on every publish. Anything written above or below its markers is kept.</p></ac:rich-text-body></ac:structured-macro>
`

// A renderer writes one section. Reasons has none: it is a switch the
// people renderer reads.
type renderer func(w io.Writer, rep sprint.Report, in Inputs, on map[string]bool) error

var renderers = map[string]renderer{
	SectionSummary:     renderSummary,
	SectionSayDo:       renderSayDo,
	SectionPeople:      renderPeople,
	SectionCalibration: renderCalibration,
	SectionStories:     renderStories,
	SectionFlags:       renderFlags,
	SectionCarryover:   renderCarryover,
	SectionCounted:     renderCounted,
}

// Render returns the block between the markers, markers included.
func Render(rep sprint.Report, in Inputs) (string, error) {
	on := make(map[string]bool, len(in.Sections))
	for _, s := range in.Sections {
		on[s] = true
	}
	var b strings.Builder
	b.WriteString(MarkerStart)
	b.WriteString("\n")
	b.WriteString(generatedNote)
	for _, id := range All {
		render, ok := renderers[id]
		if !ok || !on[id] {
			continue
		}
		if err := render(&b, rep, in, on); err != nil {
			return "", fmt.Errorf("rendering %s: %w", id, err)
		}
	}
	b.WriteString(MarkerEnd)
	return b.String(), nil
}

// tmpl parses a section template once, at package load, so a typo in
// one fails the first test rather than the first publish.
func tmpl(name, text string) *template.Template {
	return template.Must(template.New(name).Parse(text))
}

// num prints points to one decimal, or two when the value carries a
// second one: 2.5 reads as 2.5, 2.25 keeps its quarter, and 3 reads as
// 3.0 so a column of figures lines up.
func num(v float64) string {
	r := math.Round(v*100) / 100
	if r == 0 {
		// A negative zero would print its sign.
		r = 0
	}
	if math.Round(r*10)/10 != r {
		return strconv.FormatFloat(r, 'f', 2, 64)
	}
	return strconv.FormatFloat(r, 'f', 1, 64)
}

// signed is num with a plus on a positive figure, for a delta, where
// the sign is the point.
func signed(v float64) string {
	if v > 0 {
		return "+" + num(v)
	}
	return num(v)
}

// pct is a ratio as a whole percentage.
func pct(ratio float64) string {
	return strconv.Itoa(int(math.Round(ratio*100))) + "%"
}

func day(t time.Time) string { return t.Format("2 Jan 2006") }

// plural spares the page a "1 tickets".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// join reads a list out as prose: "Done", "Done or Closed", "Done,
// Closed or Shipped".
func join(items []string, last string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + last + " " + items[len(items)-1]
}
