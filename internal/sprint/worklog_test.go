package sprint

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// loggedIssue is a carried task with three entries on it, one of each
// kind the disclosure has to read: one person named, two people named
// with a figure beside each, and nobody named at all.
func loggedIssue(t *testing.T) jira.Issue {
	t.Helper()
	is := testIssue(t)
	is.Key = "ABC-2"
	is.Fields.Status.Name = "In Progress"
	lead := &jira.User{AccountID: "acc-lead", DisplayName: "The Lead"}
	at := func(day int) jira.Time { return jira.Time{Time: time.Date(2026, 1, day, 10, 0, 0, 0, time.UTC)} }
	is.Fields.Worklog.Entries = []jira.WorklogEntry{
		{ID: "500", Started: at(12), Seconds: 3 * 3600, Author: lead, Comment: json.RawMessage(
			`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[` +
				`{"type":"mention","attrs":{"id":"acc-a","text":"@Person A"}},{"type":"text","text":" reviewed the design"}]}]}`)},
		{ID: "501", Started: at(13), Seconds: 4 * 3600, Author: lead, Comment: json.RawMessage(
			`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[` +
				`{"type":"mention","attrs":{"id":"acc-a","text":"@Person A"}},{"type":"text","text":" 3h"},{"type":"hardBreak"},` +
				`{"type":"mention","attrs":{"id":"acc-lead","text":"@The Lead"}},{"type":"text","text":" 1h"}]}]}`)},
		{ID: "502", Started: at(14), Seconds: 1800, Author: lead},
	}
	is.Fields.Worklog.Total = 3
	return is
}

func TestWorklogReadsTheCachedEntries(t *testing.T) {
	svc, sp, _ := cachedService(t)
	svc.install(context.Background(), sp, []jira.Issue{testIssue(t), loggedIssue(t)},
		fetched{fields: &fields{points: testPointsField}}, time.Now().UTC())

	views, err := svc.Worklog(21, "ABC-2")
	if err != nil {
		t.Fatalf("Worklog: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(views), views)
	}

	one := views[0]
	if one.ID != "500" || one.Hours != 3 || one.Author != "acc-lead" || one.AuthorLabel != "The Lead" {
		t.Errorf("entry 500 = %+v", one)
	}
	if len(one.People) != 1 || one.People[0].ID != "acc-a" || one.People[0].Label != "Person A" {
		t.Errorf("entry 500 should credit Person A by name from the roster: %+v", one.People)
	}
	if one.Note != "reviewed the design" {
		t.Errorf("the note should be the prose with the mention taken out, got %q", one.Note)
	}
	if !one.Started.Equal(time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("started = %v", one.Started)
	}

	split := views[1]
	if len(split.People) != 2 {
		t.Fatalf("a two-name entry should name both: %+v", split.People)
	}
	// The lead is not on the roster, so the label comes from the entry's
	// own author field rather than being left as an account id.
	if split.People[1].ID != "acc-lead" || split.People[1].Label != "The Lead" {
		t.Errorf("the author's own mention should be labelled from the author: %+v", split.People[1])
	}
	if split.Note != "3h 1h" {
		t.Errorf("the figures beside the names stay in the note, got %q", split.Note)
	}

	plain := views[2]
	if len(plain.People) != 0 || plain.Hours != 0.5 || plain.Note != "" {
		t.Errorf("an entry naming nobody credits its author and says nothing: %+v", plain)
	}
}

func TestWorklogNamesWhatIsMissing(t *testing.T) {
	svc, _, _ := cachedService(t)

	if _, err := svc.Worklog(22, "ABC-1"); !errors.Is(err, ErrNoReport) {
		t.Errorf("a sprint not in the cache: err = %v, want ErrNoReport", err)
	}
	_, err := svc.Worklog(21, "ABC-9")
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("a key not in the sprint: err = %v, want ErrUnknownKey", err)
	}
	if !strings.Contains(err.Error(), "ABC-9") {
		t.Errorf("the error should name the key: %q", err)
	}

	// An issue with nothing logged answers with an empty list, not null:
	// the browser was promised an array.
	views, err := svc.Worklog(21, "ABC-1")
	if err != nil || views == nil || len(views) != 0 {
		t.Errorf("nothing logged: views = %#v, err = %v", views, err)
	}
}

func TestCommentTextLeavesOutTheMentions(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"", ""},
		{"null", ""},
		{"not json", ""},
		{`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"just  words"}]}]}`, "just words"},
		{`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"x"}}]}]}`, ""},
		{`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]},{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}`, "first second"},
	}
	for _, c := range cases {
		if got := commentText(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("commentText(%s) = %q, want %q", c.raw, got, c.want)
		}
	}
}
