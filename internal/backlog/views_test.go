package backlog

import (
	"strings"
	"testing"
)

var roster = map[string]bool{"acc-ops": true}

func TestRowsReadTheFacts(t *testing.T) {
	rows := rowsOf(t, nil, roster)
	byKey := map[string]Row{}
	for _, r := range rows {
		byKey[r.Key] = r
	}

	r := byKey["ABC-2"]
	if r.Epic == nil || r.Epic.Key != "ABC-100" || r.Epic.Summary != "[Build] Platform" {
		t.Errorf("ABC-2 epic = %+v, want ABC-100 from its parent", r.Epic)
	}
	if r.Story == nil || r.Story.Key != "ABC-50" {
		t.Errorf("ABC-2 story = %+v, want ABC-50 through the Blocks link", r.Story)
	}
	if r.Points == nil || *r.Points != 3 || r.Estimate != nil {
		t.Errorf("ABC-2 points = %v estimate = %v", r.Points, r.Estimate)
	}
	if r.Sprint == nil || r.Sprint.ID != 2 || r.Sprint.State != "active" {
		t.Errorf("ABC-2 sprint = %+v, want the last entry of the field", r.Sprint)
	}
	if r.Assignee == nil || r.Assignee.AccountID != "acc-a" || r.Reporter.Label != "Reporter One" {
		t.Errorf("ABC-2 people = %+v / %+v", r.Reporter, r.Assignee)
	}
	if r.StaleDays != 58 {
		t.Errorf("ABC-2 stale_days = %d, want 58", r.StaleDays)
	}
	if r.StatusCategory != "indeterminate" || r.URL != "https://jira.example/browse/ABC-2" {
		t.Errorf("ABC-2 category %q url %q", r.StatusCategory, r.URL)
	}

	one := byKey["ABC-1"]
	if one.Labels == nil || one.Assignee != nil || one.Epic != nil || one.Story != nil || one.Sprint != nil {
		t.Errorf("ABC-1 should be bare with an empty label list: %+v", one)
	}
	if !one.New || one.StaleDays != 0 || one.Priority != "Medium" || one.Watermark != updatedABC1 {
		t.Errorf("ABC-1 new %v stale %d priority %q watermark %q", one.New, one.StaleDays, one.Priority, one.Watermark)
	}
	// The story sits under an epic and is a container, not work.
	if st := byKey["ABC-50"]; st.Epic == nil || st.Story != nil {
		t.Errorf("ABC-50 = %+v", st)
	}
	// A sub-task's parent is a task, which is not an epic.
	if sub := byKey["ABC-6"]; !sub.Subtask || sub.Epic != nil {
		t.Errorf("ABC-6 = %+v", sub)
	}
}

func TestGroupsFireOnTheRightRows(t *testing.T) {
	rows := rowsOf(t, nil, roster)
	groups := Groups(rows, testConfig(), testFields(), roster)

	want := map[string][]string{
		"new_unacknowledged":       {"ABC-1", "ABC-4", "ABC-6"},
		"stale":                    {"ABC-2"},
		"no_epic":                  {"ABC-1"},
		"no_story":                 {"ABC-1", "ABC-3"},
		"no_labels":                {"ABC-1", "ABC-6"},
		"unsized":                  {"ABC-1"},
		"no_sprint":                {"ABC-1", "ABC-3", "ABC-50"},
		"build_epic_no_story":      {"ABC-3"},
		"operations_missing_label": {"ABC-2", "ABC-4"},
		"legacy_label":             {"ABC-2"},
		"operations_work_label":    {},
	}
	if len(groups) != len(want) {
		t.Errorf("groups = %v, want %d of them", groupIDs(groups), len(want))
	}
	for id, keys := range want {
		g := groupByID(t, groups, id)
		if !sameKeys(g.Keys, keys) {
			t.Errorf("%s = %v, want %v", id, g.Keys, keys)
		}
		if g.Why == "" || g.Label == "" {
			t.Errorf("%s has no label or reason", id)
		}
	}
}

// The story groups only make sense on a team whose hierarchy has a
// container at the standard level; without one they would list every
// task.
func TestStoryGroupsNeedAContainerType(t *testing.T) {
	f := testFields()
	f.HasContainer = false
	groups := Groups(rowsOf(t, nil, roster), testConfig(), f, roster)
	for _, id := range []string{"no_story", "build_epic_no_story"} {
		for _, g := range groups {
			if g.ID == id {
				t.Errorf("%s should be skipped when the team has no container type", id)
			}
		}
	}
	if len(groups) != 9 {
		t.Errorf("groups = %v, want the nine that remain", groupIDs(groups))
	}
}

// An acknowledgement hides a ticket exactly while its watermark matches.
func TestAcknowledgementHidesUntilTheTicketMoves(t *testing.T) {
	acked := map[string]string{"ABC-1": updatedABC1}
	rows := rowsOf(t, acked, roster)
	g := groupByID(t, Groups(rows, testConfig(), testFields(), roster), "new_unacknowledged")
	if !sameKeys(g.Keys, []string{"ABC-4", "ABC-6"}) {
		t.Errorf("acknowledged ABC-1 should be hidden: %v", g.Keys)
	}
	if g.JQL != "key in (ABC-4, ABC-6)" {
		t.Errorf("a locally decided group opens exactly its keys, got %q", g.JQL)
	}

	// The ticket changed after it was acknowledged: the watermark no
	// longer matches and it is back.
	moved := map[string]string{"ABC-1": "2026-09-20T09:00:00.000+0000"}
	rows = rowsOf(t, moved, roster)
	g = groupByID(t, Groups(rows, testConfig(), testFields(), roster), "new_unacknowledged")
	if !sameKeys(g.Keys, []string{"ABC-1", "ABC-4", "ABC-6"}) {
		t.Errorf("a changed ticket should return: %v", g.Keys)
	}
}

// A work label on a request is judged on the reporter: the same label
// on an engineer's own ticket is not a flag.
func TestWorkLabelOnARequest(t *testing.T) {
	cfg := testConfig()
	cfg.LegacyLabels, cfg.WorkLabels = nil, []string{"Ops"}
	flagged := func(roster map[string]bool) []string {
		return pick(Rows(fixture(t), cfg, nil, roster, now), func(r Row) bool { return r.WorkLabel })
	}
	// ABC-2 carries Ops and is reported by acc-r1.
	if got := flagged(map[string]bool{"acc-r1": true}); !sameKeys(got, []string{"ABC-2"}) {
		t.Errorf("flagged with the reporter on the roster = %v, want ABC-2", got)
	}
	if got := flagged(roster); len(got) != 0 {
		t.Errorf("flagged with the reporter off the roster = %v, want none", got)
	}
	ops := Operations(Rows(fixture(t), cfg, nil, map[string]bool{"acc-r1": true}, now))
	if !sameKeys(ops.WorkLabelKeys, []string{"ABC-2"}) || len(ops.LegacyLabelKeys) != 0 {
		t.Errorf("view = %+v: want ABC-2 under work labels and nothing legacy", ops)
	}
}

// Three signals make an operations request, and the missing-label list
// is the ones the label signal does not cover.
func TestOperationsBySignal(t *testing.T) {
	ops := Operations(rowsOf(t, nil, roster))
	if !sameKeys(ops.AllKeys, []string{"ABC-2", "ABC-3", "ABC-4"}) {
		t.Errorf("all = %v: want the legacy label, the label and the roster reporter", ops.AllKeys)
	}
	if !sameKeys(ops.MissingLabelKeys, []string{"ABC-2", "ABC-4"}) {
		t.Errorf("missing label = %v", ops.MissingLabelKeys)
	}
	if !sameKeys(ops.LegacyLabelKeys, []string{"ABC-2"}) {
		t.Errorf("legacy = %v", ops.LegacyLabelKeys)
	}
	// Without the roster, the reporter signal goes.
	if ops := Operations(rowsOf(t, nil, nil)); !sameKeys(ops.AllKeys, []string{"ABC-2", "ABC-3"}) {
		t.Errorf("all without a roster = %v", ops.AllKeys)
	}
}

func TestEpicsCountOpenAndUnrefined(t *testing.T) {
	snap := fixture(t)
	rows := rowsOf(t, nil, roster)
	open := []IssueRef{}
	for _, e := range snap.Epics {
		open = append(open, IssueRef{Key: e.Key, Summary: e.Fields.Summary})
	}
	epics := Epics(rows, open, testConfig())
	if len(epics) != 3 {
		t.Fatalf("epics = %+v, want three", epics)
	}
	build := epics[0]
	if build.Key != "ABC-100" || build.Class != "Build" || build.Open != 3 || build.Unrefined != 2 {
		t.Errorf("the Build epic = %+v, want 3 open of which 2 unrefined", build)
	}
	if !sameKeys(build.Keys, []string{"ABC-2", "ABC-3", "ABC-50"}) {
		t.Errorf("Build epic keys = %v", build.Keys)
	}
	if run := epics[1]; run.Key != "ABC-101" || run.Class != "Run" || run.Open != 1 || run.Unrefined != 0 {
		t.Errorf("the Run epic = %+v", run)
	}
	if empty := epics[2]; empty.Key != "ABC-102" || empty.Class != "" || empty.Open != 0 || len(empty.Keys) != 0 {
		t.Errorf("the empty epic = %+v", empty)
	}
}

// The JQL beside a group has to open the same set in the navigator, in
// the spelling the navigator accepts.
func TestGroupJQL(t *testing.T) {
	groups := Groups(rowsOf(t, nil, roster), testConfig(), testFields(), roster)
	base := "project = ABC AND statusCategory != Done"
	want := map[string]string{
		"stale":     base + " AND updated <= -14d",
		"no_labels": base + ` AND issuetype not in ("Epic") AND labels IS EMPTY`,
		"unsized": base + ` AND issuetype not in subTaskIssueTypes() AND issuetype not in ("Epic")` +
			` AND issuetype not in ("Story") AND cf[10016] IS EMPTY AND cf[10017] IS EMPTY`,
		"operations_missing_label": base + ` AND (labels in ("Ops") OR reporter in ("acc-ops"))` +
			` AND (labels IS EMPTY OR labels not in ("Operations"))`,
		"legacy_label":        base + ` AND labels in ("Ops")`,
		"no_story":            "key in (ABC-1, ABC-3)",
		"build_epic_no_story": "key in (ABC-3)",
	}
	for id, jql := range want {
		if got := groupByID(t, groups, id).JQL; got != jql {
			t.Errorf("%s jql:\n got %s\nwant %s", id, got, jql)
		}
	}
	if jql := groupByID(t, groups, "no_sprint").JQL; !strings.Contains(jql, "sprint not in openSprints()") {
		t.Errorf("no_sprint jql = %s", jql)
	}
}

func TestInboxDismissalFollowsTheWatermark(t *testing.T) {
	items := []Item{
		{ID: "jira:assigned:ABC-1", Watermark: "w1"},
		{ID: "jira:mentioned:ABC-1", Watermark: "w1"},
		{ID: "confluence:watching:42", Watermark: "2026-09-27T09:00:00.000Z"},
	}
	dismissed := map[string]string{
		"jira:assigned:ABC-1":    "w1",
		"confluence:watching:42": "2026-09-20T09:00:00.000Z",
	}
	out := Inbox(items, dismissed)
	if !out[0].Dismissed {
		t.Error("an item dismissed at its current watermark should be dismissed")
	}
	if out[1].Dismissed {
		t.Error("dismissing one kind of the same issue must not dismiss the other")
	}
	if out[2].Dismissed {
		t.Error("a page that changed since it was dismissed should be back")
	}
	if got := Inbox(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("no items should read as an empty list, got %#v", got)
	}
}
