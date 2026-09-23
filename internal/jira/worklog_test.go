package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

// A worklog comment as Jira Cloud returns it: a document holding a
// paragraph, holding mention and text nodes. The mention's id is the
// account id, which is the only thing read.
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
