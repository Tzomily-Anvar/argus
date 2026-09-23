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
// from the text on one side of it. People write both "@Person A 3h" and
// "3,5 @Person A", so the layout is decided once per comment: if the
// text before the first name ends in a figure, figures come before
// names throughout, otherwise after. Nothing else in the document is
// read.
func (w WorklogEntry) MentionShares() []Mention {
	if len(w.Comment) == 0 || string(w.Comment) == "null" {
		return nil
	}
	var root adfNode
	if err := json.Unmarshal(w.Comment, &root); err != nil {
		return nil
	}

	// Flatten to the order a reader sees: the text before the first name,
	// then each name and the text after it.
	var ids []string
	texts := []strings.Builder{{}}
	root.walk(func(n adfNode) {
		switch n.Type {
		case "mention":
			if n.Attrs.ID == "" {
				return
			}
			ids = append(ids, n.Attrs.ID)
			texts = append(texts, strings.Builder{})
		case "text":
			texts[len(texts)-1].WriteString(n.Text)
		case "hardBreak":
			texts[len(texts)-1].WriteString(" ")
		}
	})
	if len(ids) == 0 {
		return nil
	}

	_, _, before := trailingFigure(texts[0].String())
	var out []Mention
	index := map[string]int{}
	for i, id := range ids {
		var f float64
		var unit string
		var ok bool
		if before {
			f, unit, ok = trailingFigure(texts[i].String())
		} else {
			f, unit, ok = leadingFigure(texts[i+1].String())
		}
		if at, seen := index[id]; seen {
			if !out[at].HasFigure && ok {
				out[at].Figure, out[at].Unit, out[at].HasFigure = f, unit, true
			}
			continue
		}
		index[id] = len(out)
		out = append(out, Mention{ID: id, Figure: f, Unit: unit, HasFigure: ok})
	}
	return out
}

// A figure is a number with an optional unit. Leading: at the very start
// of the text after a name, after an optional separator. Trailing: at the
// very end of the text before a name. The decimal comma is accepted
// because half the world writes 3,5.
var (
	leadingPattern  = regexp.MustCompile(`^\s*[:\-–—]?\s*(\d+(?:[.,]\d+)?)\s*(h|hrs?|hours?|d|days?|sp|pts?|points?)?(?:[^A-Za-z0-9]|$)`)
	trailingPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9.,])(\d+(?:[.,]\d+)?)\s*(h|hrs?|hours?|d|days?|sp|pts?|points?)?\s*[:\-–—]?\s*$`)
)

func leadingFigure(s string) (float64, string, bool) {
	return figureFrom(leadingPattern.FindStringSubmatch(s))
}
func trailingFigure(s string) (float64, string, bool) {
	return figureFrom(trailingPattern.FindStringSubmatch(s))
}

func figureFrom(m []string) (float64, string, bool) {
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

// mentions counts the mention nodes at or beneath n, whoever they name
// and however often. A mention with no account id names nobody and is
// not counted. This is the gate's view; MentionShares above is the
// report's, which folds repeats of one person together.
func (n adfNode) mentions() int {
	count := 0
	if n.Type == "mention" && n.Attrs.ID != "" {
		count++
	}
	for _, c := range n.Content {
		count += c.mentions()
	}
	return count
}
