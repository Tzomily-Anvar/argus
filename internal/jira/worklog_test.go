package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

// A worklog comment as Jira Cloud returns it: a document holding a
// paragraph, holding mention and text nodes. The mention's id is the
// account id, which is the only thing read besides the figure beside it.
func adf(nodes ...string) json.RawMessage {
	return json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[` +
		strings.Join(nodes, ",") + `]}]}`)
}

func mention(id string) string {
	return `{"type":"mention","attrs":{"id":"` + id + `","text":"@Somebody"}}`
}

func text(s string) string { return `{"type":"text","text":"` + s + `"}` }

func TestMentionsReadsAccountIDsInOrder(t *testing.T) {
	w := WorklogEntry{Comment: adf(mention("acc-b"), text(" 3,5 "), mention("acc-a"), text(" 1"))}
	got := w.Mentions()
	if len(got) != 2 || got[0] != "acc-b" || got[1] != "acc-a" {
		t.Fatalf("Mentions() = %v, want [acc-b acc-a]", got)
	}
}

func TestMentionsIgnoresTextAndDuplicates(t *testing.T) {
	w := WorklogEntry{Comment: adf(mention("acc-a"), text(" and again "), mention("acc-a"))}
	if got := w.Mentions(); len(got) != 1 || got[0] != "acc-a" {
		t.Fatalf("Mentions() = %v, want [acc-a]", got)
	}
}

func TestMentionsOnNoCommentIsEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null"), adf(text("worked on it"))} {
		if got := (WorklogEntry{Comment: raw}).Mentions(); len(got) != 0 {
			t.Errorf("Mentions() on %q = %v, want none", raw, got)
		}
	}
}

// A malformed document is missing data, not a failure: the entry is still
// somebody's logged time and is attributed to its author.
func TestMentionsOnGarbageIsEmpty(t *testing.T) {
	if got := (WorklogEntry{Comment: json.RawMessage(`"not a document"`)}).Mentions(); len(got) != 0 {
		t.Fatalf("Mentions() = %v, want none", got)
	}
}

// The figure beside a name, in the shapes people actually type.
func TestMentionSharesReadsTheFigureBesideEachName(t *testing.T) {
	cases := []struct {
		name   string
		nodes  []string
		want   []Mention
		figure bool
	}{
		{"hours with colon", []string{mention("a"), text(": 3h "), mention("b"), text(" 1h")},
			[]Mention{{ID: "a", Figure: 3, Unit: "h", HasFigure: true}, {ID: "b", Figure: 1, Unit: "h", HasFigure: true}}, true},
		{"decimal comma, no unit", []string{mention("a"), text(" 3,5 "), mention("b"), text(" 1")},
			[]Mention{{ID: "a", Figure: 3.5, HasFigure: true}, {ID: "b", Figure: 1, HasFigure: true}}, true},
		{"days and points", []string{mention("a"), text(" 2 days, "), mention("b"), text(" 1.5sp")},
			[]Mention{{ID: "a", Figure: 2, Unit: "d", HasFigure: true}, {ID: "b", Figure: 1.5, Unit: "sp", HasFigure: true}}, true},
		{"split over lines", []string{mention("a"), text(" 4h"), `{"type":"hardBreak"}`, mention("b"), text(" 2h")},
			[]Mention{{ID: "a", Figure: 4, Unit: "h", HasFigure: true}, {ID: "b", Figure: 2, Unit: "h", HasFigure: true}}, true},
		{"prose is not a figure", []string{mention("a"), text(" worked about 3 hours with "), mention("b")},
			[]Mention{{ID: "a"}, {ID: "b"}}, false},
		{"figure only for one", []string{mention("a"), text(" 3h and "), mention("b")},
			[]Mention{{ID: "a", Figure: 3, Unit: "h", HasFigure: true}, {ID: "b"}}, true},
		{"a number that is not a figure", []string{mention("a"), text(" 2nd review "), mention("b")},
			[]Mention{{ID: "a"}, {ID: "b"}}, false},
		{"figure before the name", []string{text("3,5 "), mention("a"), text(" "), `{"type":"hardBreak"}`, text("1 "), mention("b"), text(" ")},
			[]Mention{{ID: "a", Figure: 3.5, HasFigure: true}, {ID: "b", Figure: 1, HasFigure: true}}, true},
		{"figure before, with units", []string{text("Split: 4h "), mention("a"), text(", 2h "), mention("b")},
			[]Mention{{ID: "a", Figure: 4, Unit: "h", HasFigure: true}, {ID: "b", Figure: 2, Unit: "h", HasFigure: true}}, true},
		{"prose before the name is not a figure", []string{text("Reviewed by "), mention("a"), text(" 3h")},
			[]Mention{{ID: "a", Figure: 3, Unit: "h", HasFigure: true}}, true},
	}
	for _, c := range cases {
		got := (WorklogEntry{Comment: adf(c.nodes...)}).MentionShares()
		if len(got) != len(c.want) {
			t.Errorf("%s: %d mentions, want %d: %+v", c.name, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: mention %d = %+v, want %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
}
