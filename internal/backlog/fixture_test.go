package backlog

// One backlog, decoded the way a search answer is so the raw custom
// fields are there, that every view test reads. Each ticket is built to
// land in a known set of groups, so a group firing on the wrong row is
// caught by name.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

const (
	pointsField   = "customfield_10016"
	estimateField = "customfield_10017"
	sprintField   = "customfield_10020"
)

var now = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func testConfig() Config {
	return Config{
		Project: "ABC", BaseURL: "https://jira.example",
		StaleDays: 14, NewDays: 3,
		RequestLabel: "Operations", LegacyLabels: []string{"Ops"},
		InboxDays:      3,
		StoryLinkTypes: []string{"Blocks", "migration_parent"},
		ContainerTypes: []string{"Story"},
		EpicClasses: map[string]*regexp.Regexp{
			"Run":   regexp.MustCompile(`(?i)^\[?run\]?`),
			"Build": regexp.MustCompile(`(?i)^\[?build\]?`),
		},
	}
}

func testFields() Fields {
	return Fields{Points: pointsField, Estimate: estimateField, Sprint: sprintField,
		BoardID: 1, EpicTypes: map[string]bool{"Epic": true}, HasContainer: true}
}

func issue(t *testing.T, key string, fields map[string]any) jira.Issue {
	t.Helper()
	b, err := json.Marshal(map[string]any{"id": "1" + key[4:], "key": key, "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatal(err)
	}
	return is
}

func typ(name string) map[string]any { return map[string]any{"name": name} }
func status(name, category string) map[string]any {
	return map[string]any{"name": name, "statusCategory": map[string]any{"key": category}}
}
func user(id, name string) map[string]any {
	return map[string]any{"accountId": id, "displayName": name}
}
func parent(key, summary, kind string) map[string]any {
	return map[string]any{"key": key, "fields": map[string]any{"summary": summary, "issuetype": typ(kind)}}
}
func link(kind, key, summary, issueType string) map[string]any {
	return map[string]any{"type": typ(kind), "outwardIssue": map[string]any{
		"key": key, "fields": map[string]any{"summary": summary, "issuetype": typ(issueType)}}}
}
func sprint(id int, name, state string) map[string]any {
	return map[string]any{"id": id, "name": name, "state": state}
}

// The updated timestamps, spelled as Jira spells them, because that
// spelling is the watermark.
const (
	updatedABC1 = "2026-09-27T09:00:00.000+0000"
	updatedABC4 = "2026-09-27T10:00:00.000+0000"
)

func fixture(t *testing.T) Snapshot {
	t.Helper()
	buildEpic := parent("ABC-100", "[Build] Platform", "Epic")
	runEpic := parent("ABC-101", "[Run] Support", "Epic")
	return Snapshot{
		SweptAt: now, Fields: testFields(),
		Issues: []jira.Issue{
			// New, and missing everything.
			issue(t, "ABC-1", map[string]any{
				"summary": "Fresh and bare", "issuetype": typ("Task"), "status": status("To Refine", "new"),
				"created": "2026-09-27T09:00:00.000+0000", "updated": updatedABC1,
				"reporter": user("acc-r1", "Reporter One"), "labels": []string{},
				"priority": map[string]any{"name": "Medium"},
			}),
			// Stale, under a Build epic with a story, carrying the legacy label.
			issue(t, "ABC-2", map[string]any{
				"summary": "Old bug", "issuetype": typ("Bug"), "status": status("In Progress", "indeterminate"),
				"created": "2026-08-01T09:00:00.000+0000", "updated": "2026-08-01T09:00:00.000+0000",
				"reporter": user("acc-r1", "Reporter One"), "assignee": user("acc-a", "Person A"),
				"labels": []string{"Ops"}, "parent": buildEpic,
				"issuelinks": []any{link("Blocks", "ABC-50", "The story", "Story")},
				pointsField:  3,
				sprintField:  []any{sprint(1, "Sprint 1", "closed"), sprint(2, "Sprint 2", "active")},
			}),
			// Under a Build epic with no story; labelled; rolled out of a closed sprint.
			issue(t, "ABC-3", map[string]any{
				"summary": "Build work adrift", "issuetype": typ("Task"), "status": status("Selected", "new"),
				"created": "2026-09-20T09:00:00.000+0000", "updated": "2026-09-26T09:00:00.000+0000",
				"reporter": user("acc-r2", "Reporter Two"), "labels": []string{"Operations"},
				"parent": buildEpic, estimateField: 5,
				sprintField: []any{sprint(1, "Sprint 1", "closed")},
			}),
			// New, reported by a requester, otherwise complete.
			issue(t, "ABC-4", map[string]any{
				"summary": "From operations", "issuetype": typ("Task"), "status": status("In Progress", "indeterminate"),
				"created": "2026-09-27T10:00:00.000+0000", "updated": updatedABC4,
				"reporter": user("acc-ops", "Ops Person"), "labels": []string{"backend"},
				"parent":     runEpic,
				"issuelinks": []any{link("migration_parent", "ABC-50", "The story", "Story")},
				pointsField:  2,
				sprintField:  []any{sprint(3, "Sprint 3", "future")},
			}),
			// The story itself: a container, unrefined, in no sprint.
			issue(t, "ABC-50", map[string]any{
				"summary": "The story", "issuetype": typ("Story"), "status": status("To Refine", "new"),
				"created": "2026-09-01T09:00:00.000+0000", "updated": "2026-09-20T09:00:00.000+0000",
				"reporter": user("acc-r2", "Reporter Two"), "labels": []string{"feature"}, "parent": buildEpic,
			}),
			// The open epic, which is a row too but never work.
			issue(t, "ABC-100", map[string]any{
				"summary": "[Build] Platform", "issuetype": typ("Epic"), "status": status("In Progress", "indeterminate"),
				"created": "2026-06-01T09:00:00.000+0000", "updated": "2026-09-25T09:00:00.000+0000",
				"reporter": user("acc-r2", "Reporter Two"), "labels": []string{},
			}),
			// A sub-task: its parent is a task, and it inherits the rest.
			issue(t, "ABC-6", map[string]any{
				"summary": "Part of ABC-1", "issuetype": map[string]any{"name": "Sub-task", "subtask": true},
				"status":  status("To Do", "new"),
				"created": "2026-09-27T11:00:00.000+0000", "updated": "2026-09-27T11:00:00.000+0000",
				"reporter": user("acc-r1", "Reporter One"), "labels": []string{},
				"parent": parent("ABC-1", "Fresh and bare", "Task"),
			}),
		},
		Epics: []jira.Issue{
			issue(t, "ABC-100", map[string]any{"summary": "[Build] Platform"}),
			issue(t, "ABC-101", map[string]any{"summary": "[Run] Support"}),
			issue(t, "ABC-102", map[string]any{"summary": "Nothing under it yet"}),
		},
		Sprints: []jira.Sprint{{ID: 3, Name: "Sprint 3", State: "future"}, {ID: 2, Name: "Sprint 2", State: "active"}},
	}
}

func rowsOf(t *testing.T, acks map[string]string, roster map[string]bool) []Row {
	t.Helper()
	return Rows(fixture(t), testConfig(), acks, roster, now)
}

func groupByID(t *testing.T, groups []Group, id string) Group {
	t.Helper()
	for _, g := range groups {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("no group %q in %v", id, groupIDs(groups))
	return Group{}
}

func groupIDs(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ID)
	}
	return out
}

func sameKeys(got, want []string) bool {
	return fmt.Sprint(got) == fmt.Sprint(want)
}
