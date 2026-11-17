package sprint

import (
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

func keyed(keys ...string) []jira.Issue {
	out := make([]jira.Issue, len(keys))
	for i, k := range keys {
		out[i] = jira.Issue{Key: k}
	}
	return out
}

// The label's issues are added to the sprint's, and only those the
// sprint does not hold are marked late.
func TestMergeLateAddsLabelledIssuesOnce(t *testing.T) {
	merged, late := mergeLate(keyed("ABC-1", "ABC-2"), keyed("ABC-2", "ABC-3", "ABC-3"))

	var got []string
	for _, is := range merged {
		got = append(got, is.Key)
	}
	if len(got) != 3 || got[0] != "ABC-1" || got[1] != "ABC-2" || got[2] != "ABC-3" {
		t.Fatalf("merged = %v, want ABC-1 ABC-2 ABC-3 once each", got)
	}
	if late["ABC-2"] {
		t.Error("ABC-2 is in the sprint itself, so the label does not make it late")
	}
	if !late["ABC-3"] || len(late) != 1 {
		t.Errorf("late = %v, want only ABC-3", late)
	}
}

func TestMergeLateWithNothingLabelled(t *testing.T) {
	merged, late := mergeLate(keyed("ABC-1"), nil)
	if len(merged) != 1 || late != nil {
		t.Errorf("merged = %v, late = %v; want the sprint's issues untouched and no late set", merged, late)
	}
}

func TestJQLStringEscapes(t *testing.T) {
	if got := jqlString(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("jqlString = %s", got)
	}
}
