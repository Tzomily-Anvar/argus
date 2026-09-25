package page

import (
	"errors"
	"fmt"
	"html/template"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// How Splice placed the block, so the preview can say so.
const (
	Replaced = "replaced"
	Migrated = "migrated"
	Appended = "appended"
)

// Splice puts a freshly rendered block into an existing page body:
// replacing what sits between the markers, migrating the legacy markers
// once, or appending the block when the body has neither. It reports
// which of the three it did.
//
// A body with a start marker and no end, or a marker twice, is refused
// rather than guessed at. Guessing wrong here would eat somebody's
// prose, and the page's history is the only copy of it.
func Splice(body, block string) (out string, how string, err error) {
	if err := checkBlock(block); err != nil {
		return "", "", err
	}
	switch {
	case strings.Contains(body, MarkerStart) || strings.Contains(body, MarkerEnd):
		out, err := replaceBetween(body, MarkerStart, MarkerEnd, block)
		return out, Replaced, err
	case strings.Contains(body, LegacyStart) || strings.Contains(body, LegacyEnd):
		out, err := replaceBetween(body, LegacyStart, LegacyEnd, block)
		return out, Migrated, err
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body + block, Appended, nil
}

// checkBlock refuses a block that could not be found again. Splicing
// one without both markers would leave a page Argus can never update,
// and the block is the one thing here Argus made itself.
func checkBlock(block string) error {
	if strings.Count(block, MarkerStart) != 1 || strings.Count(block, MarkerEnd) != 1 {
		return errors.New("the block must carry each marker exactly once")
	}
	if strings.Index(block, MarkerStart) > strings.Index(block, MarkerEnd) {
		return errors.New("the block's end marker comes before its start")
	}
	return nil
}

// replaceBetween swaps everything from start to the end of end for the
// block, insisting the pair is there once and in order.
func replaceBetween(body, start, end, block string) (string, error) {
	if n := strings.Count(body, start); n != 1 {
		return "", fmt.Errorf("the page holds the start marker %s %d times, expected once", start, n)
	}
	if n := strings.Count(body, end); n != 1 {
		return "", fmt.Errorf("the page holds the end marker %s %d times, expected once", end, n)
	}
	from := strings.Index(body, start)
	to := strings.Index(body, end)
	if to < from {
		return "", fmt.Errorf("the page's end marker %s comes before its start", end)
	}
	return body[:from] + block + body[to+len(end):], nil
}

// The shape of a page the team keeps by hand: one line of context with
// a place for the goal, the block, then a heading for whatever the team
// wants to say. The goal and the notes are placeholders because they
// are the reader's to write, and the block is dropped in as a whole so
// its markers are untouched by the template.
var scaffoldTmpl = tmpl("scaffold", `<p>Sprint {{.Number}}, {{.Starts}} to {{.Ends}}. <strong>Goal:</strong> <em>write the sprint goal here.</em></p>
{{.Block}}
<h2>Notes</h2>
<p><em>Anything the team wants to say about this sprint. This part of the page is never touched by a publish.</em></p>
`)

// Scaffold is a new page's body: a one-line intro with the sprint's
// dates and a Goal placeholder, the block, then a Notes heading with a
// placeholder, matching the shape of the pages the team keeps by hand.
func Scaffold(rep sprint.Report, block string) string {
	var b strings.Builder
	// The block is already storage format that this package escaped,
	// so it goes in as HTML; escaping it again would show its markup.
	err := scaffoldTmpl.Execute(&b, struct {
		Number       int
		Starts, Ends string
		Block        template.HTML
	}{
		Number: rep.Sprint.Number,
		Starts: day(rep.Sprint.Starts),
		Ends:   day(rep.Sprint.Ends),
		Block:  template.HTML(block),
	})
	if err != nil {
		// The template has no branches and three plain values; the only
		// way to get here is a bug in this file, and a scaffold with the
		// block and nothing else is better than no page.
		return block
	}
	return b.String()
}
