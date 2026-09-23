package jira

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Mention is one person named in a worklog comment, with the figure
// written beside the name if there is one.
//
// A lead logging a shared entry writes "@Person A 3h @Person B 1h", and
// the figure is what lets one Time Spent divide honestly between them.
// Only a number that immediately follows the name counts - "@Person A
// worked about 3 hours" does not - because a looser reading would start
// finding hours in prose, and the number in prose is the one that has
// already been seen to disagree with the field.
type Mention struct {
	ID        string
	Figure    float64
	Unit      string // h, d, sp, or empty when no unit was written
	HasFigure bool
}

// Mentions lists the account ids the entry's comment @mentions, in the
// order they appear, each once.
func (w WorklogEntry) Mentions() []string {
	shares := w.MentionShares()
	out := make([]string, 0, len(shares))
	for _, m := range shares {
		out = append(out, m.ID)
	}
	return out
}

// MentionShares lists the people the comment names and the figure beside
// each, in order, each account once. A person named twice keeps the first
// figure written for them.
//
// The comment is an Atlassian Document Format tree. A mention is a node
// of type "mention" whose attrs carry the account id; the figure is read
// from the text nodes that follow it, up to the next mention. Nothing
// else in the document is read.
func (w WorklogEntry) MentionShares() []Mention {
	if len(w.Comment) == 0 || string(w.Comment) == "null" {
		return nil
	}
	var root adfNode
	if err := json.Unmarshal(w.Comment, &root); err != nil {
		return nil
	}

	// Flatten to the order a reader sees: mentions and the text between.
	var out []Mention
	index := map[string]int{}
	var trailing strings.Builder
	current := -1
	flush := func() {
		if current >= 0 && !out[current].HasFigure {
			if f, unit, ok := parseFigure(trailing.String()); ok {
				out[current].Figure, out[current].Unit, out[current].HasFigure = f, unit, true
			}
		}
		trailing.Reset()
	}
	root.walk(func(n adfNode) {
		switch n.Type {
		case "mention":
			flush()
			if n.Attrs.ID == "" {
				current = -1
				return
			}
			if at, seen := index[n.Attrs.ID]; seen {
				current = at
				return
			}
			index[n.Attrs.ID] = len(out)
			out = append(out, Mention{ID: n.Attrs.ID})
			current = len(out) - 1
		case "text":
			trailing.WriteString(n.Text)
		case "hardBreak":
			trailing.WriteString(" ")
		}
	})
	flush()
	return out
}

// figurePattern is a number, optionally preceded by a separator and
// followed by a unit, at the very start of the text after a name. The
// decimal comma is accepted because half the world writes 3,5.
var figurePattern = regexp.MustCompile(`^\s*[:\-–—]?\s*(\d+(?:[.,]\d+)?)\s*(h|hrs?|hours?|d|days?|sp|pts?|points?)?(?:[^A-Za-z0-9]|$)`)

func parseFigure(s string) (float64, string, bool) {
	m := figurePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, "", false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
	if err != nil || f <= 0 {
		return 0, "", false
	}
	unit := strings.ToLower(m[2])
	switch {
	case unit == "":
	case strings.HasPrefix(unit, "h"):
		unit = "h"
	case strings.HasPrefix(unit, "d"):
		unit = "d"
	default:
		unit = "sp"
	}
	return f, unit, true
}

// adfNode is the part of an Atlassian Document Format node this package
// needs: enough to find mentions, read the text beside them, and descend.
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
	Attrs   struct {
		ID string `json:"id"`
	} `json:"attrs"`
}

func (n adfNode) walk(visit func(adfNode)) {
	visit(n)
	for _, c := range n.Content {
		c.walk(visit)
	}
}
