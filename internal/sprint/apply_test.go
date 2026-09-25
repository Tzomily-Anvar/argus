package sprint

// In the package itself, like changes_test.go: the guard comparison and
// the audit row are deliberately not part of the exported surface, and
// they are what these tests hold still.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

// Every refusal happens before the client is needed at all: the service
// here has no Jira client, and would panic if an apply reached for one.
func TestApplyRefusesBeforeAnythingIsWritten(t *testing.T) {
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	off := NewService(nil, st, Config{})
	if _, err := off.Apply(ctx, "any", "any"); !errors.Is(err, ErrWritesDisabled) {
		t.Errorf("writes off: err = %v, want ErrWritesDisabled", err)
	}
	if !strings.Contains(ErrWritesDisabled.Error(), "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Error("the refusal has to name the setting")
	}

	on := NewService(nil, st, Config{EnableWrites: true})
	if _, err := on.Apply(ctx, "nothing-here", "d"); !errors.Is(err, ErrExpired) {
		t.Errorf("unknown id: err = %v, want ErrExpired", err)
	}
	cs := on.held().Put(ChangeSet{Digest: "right", ExpiresAt: time.Now().Add(time.Hour)})
	for _, digest := range []string{"wrong", ""} {
		if _, err := on.Apply(ctx, cs.ID, digest); !errors.Is(err, ErrDigestMismatch) {
			t.Errorf("digest %q: err = %v, want ErrDigestMismatch", digest, err)
		}
	}
	if on.ChangeSet(cs.ID) == nil {
		t.Error("a refused apply must leave the preview held, so the person can try again")
	}
	if writes, _ := st.ListWrites(ctx, 0); len(writes) != 0 {
		t.Errorf("a refusal wrote %d audit rows; it must write none", len(writes))
	}
}

// issueWith builds an issue as Jira returns one, so the raw custom field
// is there to be read.
func issueWith(t *testing.T, fields map[string]any) jira.Issue {
	t.Helper()
	b, err := json.Marshal(map[string]any{"id": "10001", "key": "ABC-1", "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatal(err)
	}
	return is
}

func TestConflictSaysWhatMoved(t *testing.T) {
	rules := Rules{Done: []string{"Done"}}
	at := "2026-01-18T09:00:00.000+0000"
	previewed := time.Date(2026, 1, 18, 9, 0, 0, 0, time.UTC)
	entry := map[string]any{"id": "45", "updated": at, "timeSpentSeconds": 21600}
	base := func(over map[string]any) jira.Issue {
		fields := map[string]any{
			"updated": at, "status": map[string]any{"name": "Done"},
			"worklog": map[string]any{"worklogs": []any{entry}},
		}
		for k, v := range over {
			fields[k] = v
		}
		return issueWith(t, fields)
	}
	guarded := func(op string, was any) Change {
		return Change{Op: op, Key: "ABC-1", Guard: Guard{IssueID: "10001", Updated: previewed, Was: was, Status: "Done"}}
	}
	onEntry := func(op string, g *WorklogGuard) Change {
		return Change{Op: op, Key: "ABC-1", WorklogID: "45", Guard: Guard{IssueID: "10001", Entry: g}}
	}
	moved := time.Date(2026, 1, 19, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		change  Change
		issue   jira.Issue
		touched bool
		added   []string
		want    string // a fragment of the reason, or "" for no conflict
	}{
		{"nothing moved", guarded(OpPointsSet, nil), base(nil), false, nil, ""},
		{"updated moved", guarded(OpPointsSet, nil), base(map[string]any{"updated": "2026-01-19T09:00:00.000+0000"}), false, nil, "since the preview"},
		{"updated moved by this apply", guarded(OpPointsSet, nil), base(map[string]any{"updated": "2026-01-19T09:00:00.000+0000"}), true, nil, ""},
		{"points filled in meanwhile", guarded(OpPointsSet, nil), base(map[string]any{testPointsField: 3.0}), true, nil, "now holds 3, not empty"},
		{"reversal finds what was written", Change{Op: OpPointsSet, Guard: Guard{Was: 3.0}}, base(map[string]any{testPointsField: 3.0}), false, nil, ""},
		{"reversal finds another value", Change{Op: OpPointsSet, Guard: Guard{Was: 3.0}}, base(map[string]any{testPointsField: 5.0}), false, nil, "now holds 5, not 3"},
		{"reopened", guarded(OpPointsSet, nil), base(map[string]any{"status": map[string]any{"name": "In Progress"}}), true, nil, "status moved from Done to In Progress"},
		{"assigned meanwhile", guarded(OpAssigneeSet, nil), base(map[string]any{"assignee": map[string]any{"accountId": "acc-b"}}), true, nil, "assignee is now acc-b, not empty"},
		{"reversal finds the assignee it set", Change{Op: OpAssigneeSet, Guard: Guard{Was: map[string]string{"accountId": "acc-a"}}}, base(map[string]any{"assignee": map[string]any{"accountId": "acc-a"}}), false, nil, ""},
		{"worklog unchanged", guarded(OpWorklogAdd, []string{"45"}), base(nil), false, nil, ""},
		{"time logged meanwhile", guarded(OpWorklogAdd, []string{"45"}), base(map[string]any{"worklog": map[string]any{"worklogs": []any{entry, map[string]any{"id": "46"}}}}), true, nil, "time was logged"},
		{"entry added by this apply", guarded(OpWorklogAdd, []string{"45"}), base(map[string]any{"worklog": map[string]any{"worklogs": []any{entry, map[string]any{"id": "46"}}}}), true, []string{"46"}, ""},
		{"reversal add has no expectation", Change{Op: OpWorklogAdd, Guard: Guard{}}, base(nil), false, nil, ""},
		{"entry unchanged", onEntry(OpWorklogUpdate, &WorklogGuard{ID: "45", Updated: previewed, Seconds: 21600}), base(nil), false, nil, ""},
		{"entry edited", onEntry(OpWorklogDelete, &WorklogGuard{ID: "45", Updated: moved, Seconds: 21600}), base(nil), false, nil, "was edited since the preview"},
		{"reversal finds other hours", onEntry(OpWorklogUpdate, &WorklogGuard{ID: "45", Seconds: 7200}), base(nil), false, nil, "now holds 6h, not the 2h previewed"},
		{"entry gone", Change{Op: OpWorklogDelete, WorklogID: "99", Guard: Guard{Entry: &WorklogGuard{ID: "99"}}}, base(nil), false, nil, "no longer on the issue"},
	}
	for _, c := range cases {
		got := conflict(c.change, c.issue, testPointsField, rules, c.touched, c.added)
		switch {
		case c.want == "" && got != "":
			t.Errorf("%s: unexpected conflict %q", c.name, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s: conflict = %q, want it to mention %q", c.name, got, c.want)
		}
	}
}

// Each operation becomes exactly the request the gate lists: the path,
// the query and the body. An unknown operation becomes no request.
func TestRequestIsTheShapeTheGateAccepts(t *testing.T) {
	body := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	cases := []struct {
		change              Change
		method, path, query string
		body                string
	}{
		{Change{Op: OpPointsSet, Key: "ABC-1", After: 3.0}, http.MethodPut, "/rest/api/3/issue/ABC-1", "", `{"fields":{"customfield_10016":3}}`},
		{Change{Op: OpPointsSet, Key: "ABC-1", After: nil}, http.MethodPut, "/rest/api/3/issue/ABC-1", "", `{"fields":{"customfield_10016":null}}`},
		{Change{Op: OpAssigneeSet, Key: "ABC-1", After: map[string]string{"accountId": "acc-a"}}, http.MethodPut, "/rest/api/3/issue/ABC-1", "", `{"fields":{"assignee":{"accountId":"acc-a"}}}`},
		{Change{Op: OpWorklogAdd, Key: "ABC-2", After: map[string]any{"timeSpentSeconds": 3600}}, http.MethodPost, "/rest/api/3/issue/ABC-2/worklog", worklogQuery, `{"timeSpentSeconds":3600}`},
		{Change{Op: OpWorklogUpdate, Key: "ABC-2", WorklogID: "45", After: map[string]any{"timeSpentSeconds": 3600}}, http.MethodPut, "/rest/api/3/issue/ABC-2/worklog/45", worklogQuery, `{"timeSpentSeconds":3600}`},
		{Change{Op: OpWorklogDelete, Key: "ABC-2", WorklogID: "45"}, http.MethodDelete, "/rest/api/3/issue/ABC-2/worklog/45", worklogQuery, "null"},
		{Change{Op: "status.transition", Key: "ABC-2"}, "", "", "", "null"},
	}
	for _, c := range cases {
		method, path, query, got := request(c.change, testPointsField)
		if method != c.method || path != c.path || query != c.query || body(got) != c.body {
			t.Errorf("%s: %s %s ?%s %s", c.change.Op, method, path, query, body(got))
		}
	}
}

// Where the edit screen lacks the points field, the apply goes the way
// the backlog view does: the board's estimation endpoint, value as a
// string, nil to clear. Types whose screen carries the field keep the
// plain edit.
func TestPointsGoThroughTheBoardWhenTheScreenLacksTheField(t *testing.T) {
	a := &applier{points: testPointsField, board: 42, viaBoard: map[string]bool{"Story": true}}
	body := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	method, path, query, got := a.request(Change{Op: OpPointsSet, Key: "ABC-1", Type: "Story", After: 6.2})
	if method != http.MethodPut || path != "/rest/agile/1.0/issue/ABC-1/estimation" || query != "boardId=42" || body(got) != `{"value":"6.2"}` {
		t.Errorf("story points via the board: %s %s ?%s %s", method, path, query, body(got))
	}
	_, _, _, got = a.request(Change{Op: OpPointsSet, Key: "ABC-1", Type: "Story", After: nil})
	if body(got) != `{"value":null}` {
		t.Errorf("a clear via the board should send null, got %s", body(got))
	}
	method, path, query, got = a.request(Change{Op: OpPointsSet, Key: "ABC-2", Type: "Task", After: 3.0})
	if method != http.MethodPut || path != "/rest/api/3/issue/ABC-2" || query != "" || body(got) != `{"fields":{"customfield_10016":3}}` {
		t.Errorf("task points stay a plain edit: %s %s ?%s %s", method, path, query, body(got))
	}
}

// The reversal is the audit log read backwards: each applied row becomes
// the change that undoes it, guarded on what the row says was written.
func TestReversalInvertsTheLog(t *testing.T) {
	note := "finished with no estimate; digest abc; sprint 744; issue 10001"
	rows := []store.WriteRecord{
		{Operation: OpPointsSet, Target: "ABC-1", After: "3", ChangeSet: "cs-1", Note: note},
		{Operation: OpPointsSet, Target: "ABC-9", Before: "5", After: "8", ChangeSet: "cs-1", Note: "story wrapped up; digest abc; sprint 744; issue 10009"},
		{Operation: OpAssigneeSet, Target: "ABC-2", After: "acc-a", ChangeSet: "cs-1", Note: "finished unassigned; digest abc; sprint 744; issue 10002"},
		{Operation: OpWorklogAdd, Target: "ABC-3", After: "6", ChangeSet: "cs-1", Note: "carried with no time logged; digest abc; sprint 744; issue 10003; entry 46; person acc-a; started 2026-01-19T17:00:00Z"},
		{Operation: OpWorklogUpdate, Target: "ABC-3", Before: "2", After: "3", ChangeSet: "cs-1", Note: "carried over, an entry corrected; digest abc; sprint 744; issue 10003; entry 45; person acc-a; started 2026-01-19T17:00:00Z"},
		{Operation: OpWorklogDelete, Target: "ABC-4", Before: "4", ChangeSet: "cs-1", Note: "carried over, an entry removed; digest abc; sprint 744; issue 10004; entry 44; person acc-a; started 2026-01-19T17:00:00Z"},
		{Operation: OpWorklogDelete, Target: "ABC-5", Before: "4", ChangeSet: "cs-1", Note: "carried over, an entry removed; digest abc; sprint 744; issue 10005; entry 43"},
	}
	people := []store.Person{{AccountID: "acc-a", Name: "Person A", Active: true}}
	now := time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC)
	cs := reversal(rows, testPointsField, people, now, "operator@example.com")

	if cs.SprintJiraID != 744 || cs.Digest == "" || cs.Actor != "operator@example.com" || !cs.ExpiresAt.Equal(now.Add(ChangeSetLifetime)) {
		t.Errorf("header = sprint %d digest %q actor %q expires %v", cs.SprintJiraID, cs.Digest, cs.Actor, cs.ExpiresAt)
	}
	if len(cs.Changes) != 6 || len(cs.Skipped) != 1 {
		t.Fatalf("want six changes and one skip, got %d and %d: %+v", len(cs.Changes), len(cs.Skipped), cs.Skipped)
	}
	if !strings.Contains(cs.Skipped[0].Reason, "named no single person") || cs.Skipped[0].Key != "ABC-5" {
		t.Errorf("skip = %+v", cs.Skipped[0])
	}

	c := cs.Changes[0]
	if c.Op != OpPointsSet || c.Field != testPointsField || c.After != nil || c.Guard.Was != 3.0 || c.Guard.IssueID != "10001" || c.AfterLabel != "(empty)" {
		t.Errorf("points reversal = %+v", c)
	}
	if !strings.Contains(c.Reason, "reverses points.set from change set cs-1") {
		t.Errorf("reason = %q", c.Reason)
	}
	if c = cs.Changes[1]; c.After != 5.0 || c.Guard.Was != 8.0 || c.AfterLabel != "5" {
		t.Errorf("a rollup goes back to what it held: %+v", c)
	}
	c = cs.Changes[2]
	if c.Op != OpAssigneeSet || c.Field != "assignee" || c.After != nil || accountOf(c.Guard.Was) != "acc-a" || c.AfterLabel != "(unassigned)" {
		t.Errorf("assignee reversal = %+v", c)
	}
	c = cs.Changes[3]
	if c.Op != OpWorklogDelete || c.WorklogID != "46" || c.Guard.Entry == nil || c.Guard.Entry.Seconds != 21600 || c.Hours != 6 || c.PersonLabel != "Person A" {
		t.Errorf("an add reverses to a delete of the recorded id: %+v", c)
	}
	c = cs.Changes[4]
	if c.Op != OpWorklogUpdate || c.WorklogID != "45" || c.Guard.Entry == nil || c.Guard.Entry.Seconds != 10800 {
		t.Errorf("an update reverses to an update guarded on what was written: %+v", c)
	}
	if b, _ := json.Marshal(c.After); string(b) != `{"timeSpentSeconds":7200}` {
		t.Errorf("update body = %s", b)
	}
	c = cs.Changes[5]
	if c.Op != OpWorklogAdd || c.Guard.Was != nil || c.Person != "acc-a" || c.Hours != 4 || !strings.Contains(c.Reason, "new id") {
		t.Errorf("a delete reverses to an add that says it is a new entry: %+v", c)
	}
	after, _ := c.After.(map[string]any)
	if after["timeSpentSeconds"] != 14400 || after["started"] != "2026-01-19T17:00:00.000+0000" || after["comment"] == nil {
		t.Errorf("re-add body = %v", after)
	}
}

func TestAppliedRowsFilterOneSetOldestFirst(t *testing.T) {
	all := []store.WriteRecord{ // newest first, as the store lists them
		{ID: 5, Target: "ABC-5", ChangeSet: "cs-2", Outcome: store.OutcomeApplied},
		{ID: 4, Target: "ABC-4", ChangeSet: "cs-1", Outcome: store.OutcomeSkipped},
		{ID: 3, Target: "ABC-3", ChangeSet: "cs-1", Outcome: store.OutcomeApplied},
		{ID: 2, Target: "ABC-2", ChangeSet: "", Outcome: ""},
		{ID: 1, Target: "ABC-1", ChangeSet: "cs-1", Outcome: store.OutcomeApplied},
	}
	rows := appliedRows(all, "cs-1")
	if len(rows) != 2 || rows[0].Target != "ABC-1" || rows[1].Target != "ABC-3" {
		t.Errorf("rows = %+v", rows)
	}
	if appliedRows(all, "") != nil || appliedRows(all, "cs-9") != nil {
		t.Error("no set, or an unknown one, has no rows")
	}
}

func TestNoteFactsReadsWhatNoteWrites(t *testing.T) {
	a := &applier{cs: ChangeSet{ID: "cs-1", Digest: "abc", SprintJiraID: 744}}
	c := Change{
		Op: OpWorklogAdd, Reason: "carried with no time logged", Person: "acc-a",
		Started: time.Date(2026, 1, 19, 18, 0, 0, 0, time.FixedZone("CET", 3600)),
		Guard:   Guard{IssueID: "10003"},
	}
	note := a.note(c, store.OutcomeApplied, "", "46")
	want := "carried with no time logged; digest abc; sprint 744; issue 10003; entry 46; person acc-a; started 2026-01-19T17:00:00Z"
	if note != want {
		t.Errorf("note = %q\nwant   %q", note, want)
	}
	facts := noteFacts(note)
	for k, v := range map[string]string{"digest": "abc", "sprint": "744", "issue": "10003", "entry": "46", "person": "acc-a", "started": "2026-01-19T17:00:00Z"} {
		if facts[k] != v {
			t.Errorf("facts[%s] = %q, want %q", k, facts[k], v)
		}
	}
	// A skipped row carries its reason, and the reason is prose.
	skipped := a.note(Change{Op: OpPointsSet, Reason: "finished with no estimate"}, store.OutcomeSkipped, "edited in Jira since the preview", "")
	if !strings.HasSuffix(skipped, "skipped: edited in Jira since the preview") || len(noteFacts(skipped)) != 2 {
		t.Errorf("skipped note = %q, facts %v", skipped, noteFacts(skipped))
	}
}
