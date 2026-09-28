package jira

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// The Backlog tool's writes: labels added or removed, the parent set or
// cleared, one link made or removed, issues moved onto a sprint or back
// to the backlog, and an issue deleted - the last behind a second
// setting. Each is an exact path, an exact query and an exact body.

const (
	ticket      = "/rest/api/3/issue/PRJ-7"
	links       = "/rest/api/3/issueLink"
	oneLink     = links + "/314"
	sprintMove  = "/rest/agile/1.0/sprint/9/issue"
	backlogMove = "/rest/agile/1.0/backlog/issue"
)

var deletes = writePolicy{Allowed: true, PointsField: "customfield_10000", Delete: true}

func labels(ops ...map[string]any) map[string]any {
	return map[string]any{"update": map[string]any{"labels": ops}}
}

func parent(v any) map[string]any {
	return map[string]any{"fields": map[string]any{"parent": v}}
}

func link(typ, inward, outward string) map[string]any {
	return map[string]any{
		"type":         map[string]any{"name": typ},
		"inwardIssue":  map[string]any{"key": inward},
		"outwardIssue": map[string]any{"key": outward},
	}
}

func issues(keys ...string) map[string]any { return map[string]any{"issues": keys} }

func manyKeys(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, "PRJ-"+strconv.Itoa(i))
	}
	return out
}

func TestBacklogWritesAreTheOnesListed(t *testing.T) {
	mustAllow(t, assertPermitted(http.MethodPut, ticket, "notifyUsers=false", labels(map[string]any{"add": "Operations"}), on), "adding a label")
	mustAllow(t, assertPermitted(http.MethodPut, ticket, "notifyUsers=false",
		labels(map[string]any{"add": "Operations"}, map[string]any{"remove": "Ops"}), on), "a migration")
	mustAllow(t, assertPermitted(http.MethodPut, ticket, "notifyUsers=false", parent(map[string]any{"key": "PRJ-1"}), on), "setting the parent")
	mustAllow(t, assertPermitted(http.MethodPut, ticket, "notifyUsers=false", parent(nil), on), "clearing the parent")
	mustAllow(t, assertPermitted(http.MethodPost, links, "", link("Blocks", "PRJ-7", "PRJ-2"), on), "creating a link")
	mustAllow(t, assertPermitted(http.MethodDelete, oneLink, "", nil, on), "removing a link")
	mustAllow(t, assertPermitted(http.MethodPost, sprintMove, "", issues("PRJ-7", "PRJ-8"), on), "moving onto a sprint")
	mustAllow(t, assertPermitted(http.MethodPost, sprintMove, "", issues(manyKeys(50)...), on), "moving fifty")
	mustAllow(t, assertPermitted(http.MethodPost, backlogMove, "", issues("PRJ-7"), on), "moving to the backlog")
	mustAllow(t, assertPermitted(http.MethodDelete, ticket, "deleteSubtasks=true", nil, deletes), "deleting with the flag")

	for _, req := range []request{
		{http.MethodPut, ticket, "notifyUsers=false", labels(map[string]any{"add": "Operations"})},
		{http.MethodPut, ticket, "notifyUsers=false", parent(map[string]any{"key": "PRJ-1"})},
		{http.MethodPost, links, "", link("Blocks", "PRJ-7", "PRJ-2")},
		{http.MethodDelete, oneLink, "", nil},
		{http.MethodPost, sprintMove, "", issues("PRJ-7")},
		{http.MethodPost, backlogMove, "", issues("PRJ-7")},
		{http.MethodDelete, ticket, "deleteSubtasks=true", nil},
	} {
		var disabled *WritesDisabledError
		if err := assertPermitted(req.method, req.path, req.query, req.body, off); !errors.As(err, &disabled) {
			t.Errorf("%s %s with writes off: got %v, want *WritesDisabledError", req.method, req.path, err)
		}
	}
}

// Deleting an issue answers to its own setting on top of the general
// one, and the refusal names it. Nothing else under DELETE is touched by
// the flag: a worklog entry or a link goes without it.
func TestDeletingAnIssueNeedsItsOwnSetting(t *testing.T) {
	var disabled *DeletesDisabledError
	err := assertPermitted(http.MethodDelete, ticket, "deleteSubtasks=true", nil, on)
	if !errors.As(err, &disabled) || !strings.Contains(err.Error(), "ARGUS_BACKLOG_ALLOW_DELETE") {
		t.Errorf("delete without the flag: got %v, want *DeletesDisabledError naming the setting", err)
	}
	var writesOff *WritesDisabledError
	if err := assertPermitted(http.MethodDelete, ticket, "deleteSubtasks=true", nil, writePolicy{Delete: true}); !errors.As(err, &writesOff) {
		t.Errorf("delete with writes off: got %v, want *WritesDisabledError", err)
	}
	mustAllow(t, assertPermitted(http.MethodDelete, oneLink, "", nil, on), "removing a link without the flag")
	mustAllow(t, assertPermitted(http.MethodDelete, entry, worklogQuery, nil, on), "removing a worklog entry without the flag")
}

func TestBacklogBodiesAndPathsAreExact(t *testing.T) {
	refused := []struct {
		name  string
		req   request
		allow writePolicy
	}{
		{"labels with no operation", request{http.MethodPut, ticket, "notifyUsers=false", labels()}, on},
		{"labels with an empty label", request{http.MethodPut, ticket, "notifyUsers=false", labels(map[string]any{"add": ""})}, on},
		{"labels with a set", request{http.MethodPut, ticket, "notifyUsers=false", labels(map[string]any{"set": []string{"x"}})}, on},
		{"labels with add and remove in one", request{http.MethodPut, ticket, "notifyUsers=false", labels(map[string]any{"add": "x", "remove": "y"})}, on},
		{"labels beside fields", request{http.MethodPut, ticket, "notifyUsers=false", map[string]any{
			"update": map[string]any{"labels": []map[string]any{{"add": "x"}}}, "fields": map[string]any{}}}, on},
		{"an update of another field", request{http.MethodPut, ticket, "notifyUsers=false", map[string]any{
			"update": map[string]any{"summary": []map[string]any{{"set": "x"}}}}}, on},
		{"labels without the query", request{http.MethodPut, ticket, "", labels(map[string]any{"add": "x"})}, on},
		{"labels with notifications on", request{http.MethodPut, ticket, "notifyUsers=true", labels(map[string]any{"add": "x"})}, on},
		{"labels with an extra parameter", request{http.MethodPut, ticket, "notifyUsers=false&overrideScreenSecurity=true", labels(map[string]any{"add": "x"})}, on},
		{"labels on the transitions path", request{http.MethodPut, ticket + "/transitions", "notifyUsers=false", labels(map[string]any{"add": "x"})}, on},
		{"a parent by id", request{http.MethodPut, ticket, "notifyUsers=false", parent(map[string]any{"id": "10001"})}, on},
		{"a parent with a second key", request{http.MethodPut, ticket, "notifyUsers=false", parent(map[string]any{"key": "PRJ-1", "id": "1"})}, on},
		{"a parent that is not a key", request{http.MethodPut, ticket, "notifyUsers=false", parent(map[string]any{"key": "10001"})}, on},
		{"a parent without the query", request{http.MethodPut, ticket, "", parent(map[string]any{"key": "PRJ-1"})}, on},
		{"a parent beside another field", request{http.MethodPut, ticket, "notifyUsers=false", map[string]any{
			"fields": map[string]any{"parent": map[string]any{"key": "PRJ-1"}, "summary": "x"}}}, on},
		{"a link to itself", request{http.MethodPost, links, "", link("Blocks", "PRJ-7", "PRJ-7")}, on},
		{"a link with no type", request{http.MethodPost, links, "", link("", "PRJ-7", "PRJ-2")}, on},
		{"a link with a comment", request{http.MethodPost, links, "", map[string]any{
			"type": map[string]any{"name": "Blocks"}, "inwardIssue": map[string]any{"key": "PRJ-7"},
			"outwardIssue": map[string]any{"key": "PRJ-2"}, "comment": map[string]any{"body": "x"}}}, on},
		{"a link by id", request{http.MethodPost, links, "", link("Blocks", "10001", "PRJ-2")}, on},
		{"a link with a query", request{http.MethodPost, links, "notifyUsers=false", link("Blocks", "PRJ-7", "PRJ-2")}, on},
		{"a link type by id", request{http.MethodPost, links, "", map[string]any{
			"type": map[string]any{"id": "10000"}, "inwardIssue": map[string]any{"key": "PRJ-7"}, "outwardIssue": map[string]any{"key": "PRJ-2"}}}, on},
		{"removing a link with a body", request{http.MethodDelete, oneLink, "", map[string]any{}}, on},
		{"removing a link with a query", request{http.MethodDelete, oneLink, "notifyUsers=false", nil}, on},
		{"removing a link by key", request{http.MethodDelete, links + "/PRJ-7", "", nil}, on},
		{"fifty-one keys", request{http.MethodPost, sprintMove, "", issues(manyKeys(51)...)}, on},
		{"no keys", request{http.MethodPost, sprintMove, "", issues()}, on},
		{"a key that is an id", request{http.MethodPost, sprintMove, "", issues("10001")}, on},
		{"issues beside another key", request{http.MethodPost, sprintMove, "", map[string]any{"issues": []string{"PRJ-7"}, "rankBeforeIssue": "PRJ-1"}}, on},
		{"a move with a query", request{http.MethodPost, sprintMove, "notifyUsers=false", issues("PRJ-7")}, on},
		{"the sprint itself", request{http.MethodPost, "/rest/agile/1.0/sprint/9", "", issues("PRJ-7")}, on},
		{"editing the sprint", request{http.MethodPut, "/rest/agile/1.0/sprint/9", "", map[string]any{"state": "closed"}}, on},
		{"a sprint move by PUT", request{http.MethodPut, sprintMove, "", issues("PRJ-7")}, on},
		{"a backlog move with a query", request{http.MethodPost, backlogMove, "boardId=1", issues("PRJ-7")}, on},
		{"a board's backlog", request{http.MethodPost, "/rest/agile/1.0/backlog/1/issue", "", issues("PRJ-7")}, on},
		{"a delete without the query", request{http.MethodDelete, ticket, "", nil}, deletes},
		{"a delete keeping subtasks", request{http.MethodDelete, ticket, "deleteSubtasks=false", nil}, deletes},
		{"a delete with a body", request{http.MethodDelete, ticket, "deleteSubtasks=true", map[string]any{}}, deletes},
		{"a delete by id", request{http.MethodDelete, "/rest/api/3/issue/10001", "deleteSubtasks=true", nil}, deletes},
		{"a delete beneath the issue", request{http.MethodDelete, ticket + "/properties/x", "", nil}, deletes},
		{"a transition", request{http.MethodPost, ticket + "/transitions", "", map[string]any{"transition": map[string]any{"id": "31"}}}, deletes},
		{"a comment", request{http.MethodPost, ticket + "/comment", "", map[string]any{"body": "x"}}, deletes},
		{"a link update", request{http.MethodPut, oneLink, "", link("Blocks", "PRJ-7", "PRJ-2")}, deletes},
		{"creating an issue", request{http.MethodPost, "/rest/api/3/issue", "", map[string]any{"fields": map[string]any{}}}, deletes},
	}
	for _, c := range refused {
		mustRefuse(t, assertPermitted(c.req.method, c.req.path, c.req.query, c.req.body, c.allow), c.name)
	}
}
