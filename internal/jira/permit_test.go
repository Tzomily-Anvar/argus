package jira

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// The write gate, with writes switched on. Every test here uses the same
// site: points live in customfield_10000, and the issue is ABC-123.

var on = writePolicy{Allowed: true, PointsField: "customfield_10000"}

const (
	issue   = "/rest/api/3/issue/ABC-123"
	worklog = issue + "/worklog"
	entry   = worklog + "/45"
)

func points(n any) map[string]any {
	return map[string]any{"fields": map[string]any{"customfield_10000": n}}
}

func assignee(v any) map[string]any {
	return map[string]any{"fields": map[string]any{"assignee": v}}
}

func mention(id string) map[string]any {
	return map[string]any{"type": "mention", "attrs": map[string]any{"id": id}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

// adf is a one-paragraph Atlassian document holding the given inline nodes.
func adf(inline ...any) map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "paragraph", "content": inline},
	}}
}

// logged is a well-formed worklog entry: six hours for one person.
func logged() map[string]any {
	return map[string]any{
		"started":          "2026-09-19T17:00:00.000+0000",
		"timeSpentSeconds": 21600,
		"comment":          adf(mention("account-a"), text(" reviewed the design")),
	}
}

func mustAllow(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s was refused: %v", what, err)
	}
}

// mustRefuse asserts a *WriteAttemptError: the request was never on the
// list, or failed the list's checks, and no setting changes that.
func mustRefuse(t *testing.T, err error, what string) {
	t.Helper()
	var attempt *WriteAttemptError
	if !errors.As(err, &attempt) {
		t.Errorf("%s: got %v, want *WriteAttemptError", what, err)
	}
}

// Writes are refused outright unless this deployment asked for them. The
// setting is checked here, at the gate, and nowhere else; a call site
// that checked it could also forget to. Every one of these is a request
// the gate would pass with writes on.
func TestWritesRefusedWhenDisabled(t *testing.T) {
	off := writePolicy{Allowed: false, PointsField: "customfield_10000"}
	for _, c := range wellFormed() {
		err := assertPermitted(c.method, c.path, c.query, c.body, off)
		var disabled *WritesDisabledError
		if !errors.As(err, &disabled) {
			t.Errorf("%s %s: got %v, want *WritesDisabledError", c.method, c.path, err)
			continue
		}
		if !strings.Contains(err.Error(), "ARGUS_SPRINT_ALLOW_WRITES") {
			t.Errorf("the refusal has to name the setting: %q", err)
		}
	}
}

// With writes on, the same requests go through. This is the whole of what
// the client can write.
func TestListedWritesAreAllowed(t *testing.T) {
	for _, c := range wellFormed() {
		mustAllow(t, assertPermitted(c.method, c.path, c.query, c.body, on), c.method+" "+c.path)
	}
}

type request struct {
	method, path, query string
	body                any
}

func wellFormed() []request {
	return []request{
		{http.MethodPut, issue, "", points(3.0)},
		{http.MethodPut, issue, "", assignee(map[string]any{"accountId": "account-a"})},
		{http.MethodPost, worklog, worklogQuery, logged()},
		{http.MethodPut, entry, worklogQuery, map[string]any{"timeSpentSeconds": 7200}},
		{http.MethodDelete, entry, worklogQuery, nil},
	}
}

// With writes on, the list is still a list. Everything not on it is
// refused, and each of these is a field somebody would be upset to find
// edited by a reporting tool.
func TestOnlyTheListedFieldsAreWritable(t *testing.T) {
	bad := []map[string]any{
		{"fields": map[string]any{"summary": "rewritten"}},
		{"fields": map[string]any{"description": "rewritten"}},
		{"fields": map[string]any{"labels": []string{"x"}}},
		{"fields": map[string]any{"duedate": "2026-01-01"}},
		{"fields": map[string]any{"parent": map[string]any{"key": "ABC-1"}}},
		{"fields": map[string]any{"customfield_10000": 3.0, "summary": "sneaked in"}},
		{"fields": map[string]any{"customfield_99999": 3.0}}, // not this site's points field
		{"fields": map[string]any{}},
		{"fields": map[string]any{"customfield_10000": 3.0}, "update": map[string]any{}},
		{"update": map[string]any{"comment": []any{}}},
		{"transition": map[string]any{"id": "31"}},
		{"properties": []any{}},
		{"historyMetadata": map[string]any{}},
		points("3"),  // a string is not a number
		points(nil),  // clearing is not setting
		points(true), // nor is this
		assignee(map[string]any{"accountId": ""}),
		assignee(map[string]any{"accountId": "account-a", "name": "Person A"}),
		assignee(map[string]any{"name": "Person A"}),
		assignee("account-a"),
		assignee(nil),
	}
	for _, body := range bad {
		mustRefuse(t, assertPermitted(http.MethodPut, issue, "", body, on), "PUT issue with body")
	}
	mustRefuse(t, assertPermitted(http.MethodPut, issue, "", nil, on), "PUT issue with no body")
	mustRefuse(t, assertPermitted(http.MethodPut, issue, "", []any{}, on), "PUT issue with an array")
	mustRefuse(t, assertPermitted(http.MethodPut, issue, "notifyUsers=false", points(3.0), on), "PUT issue with a query")

	// A site that never resolved its points field cannot set points, even
	// to a body that names a plausible id.
	noField := writePolicy{Allowed: true}
	mustRefuse(t, assertPermitted(http.MethodPut, issue, "", points(3.0), noField), "points with no field configured")
}

// The path is exact. Everything beneath an issue is a different API with
// different consequences, and a prefix match would let all of them in.
func TestWritePathIsExact(t *testing.T) {
	for _, p := range []string{
		"/rest/api/3/issue/ABC-123/worklog", // on the list, but for POST
		"/rest/api/3/issue/ABC-123/transitions",
		"/rest/api/3/issue/ABC-123/comment",
		"/rest/api/3/issue/ABC-123/attachments",
		"/rest/api/3/issue/ABC-123/worklog/45/x",
		"/rest/api/3/issue/ABC-123/",
		"/rest/api/3/issue/ABC-123?notifyUsers=false",
		"/rest/api/3/issue/abc-123",
		"/rest/api/3/issue/10001", // a numeric id is not a key
		"/rest/api/3/issue/bulk",
		"/rest/api/3/issue",
		"/rest/api/2/issue/ABC-123",
		"/wiki/api/v2/pages/123",
	} {
		mustRefuse(t, assertPermitted(http.MethodPut, p, "", points(3.0), on), "PUT "+p)
	}
	for _, p := range []string{entry, entry + "/x", "/rest/api/3/issue/ABC-123/worklog/", issue} {
		mustRefuse(t, assertPermitted(http.MethodPost, p, worklogQuery, logged(), on), "POST "+p)
	}
	for _, p := range []string{worklog, entry + "/x", issue} {
		mustRefuse(t, assertPermitted(http.MethodDelete, p, worklogQuery, nil, on), "DELETE "+p)
	}
}

// The gate judges the request Jira will see, not the Go value it came
// from. A struct and a map that encode the same are the same request;
// a struct tag that renames a field is judged by the name on the wire.
func TestStructAndMapAreJudgedAlike(t *testing.T) {
	type pointsEdit struct {
		Fields struct {
			Points float64 `json:"customfield_10000"`
		} `json:"fields"`
	}
	var edit pointsEdit
	edit.Fields.Points = 3
	mustAllow(t, assertPermitted(http.MethodPut, issue, "", edit, on), "points as a struct")
	mustAllow(t, assertPermitted(http.MethodPut, issue, "", &edit, on), "points as a pointer to a struct")
	mustAllow(t, assertPermitted(http.MethodPut, issue, "", points(3.0), on), "points as a map")

	type widerEdit struct {
		Fields struct {
			Points  float64 `json:"customfield_10000"`
			Summary string  `json:"summary"`
		} `json:"fields"`
	}
	var wider widerEdit
	wider.Fields.Points = 3
	mustRefuse(t, assertPermitted(http.MethodPut, issue, "", wider, on), "a struct with a second field")
	mustRefuse(t, assertPermitted(http.MethodPut, issue, "", map[string]any{"fields": map[string]any{
		"customfield_10000": 3.0, "summary": ""}}, on), "a map with a second field")

	type worklogAdd struct {
		Started string         `json:"started"`
		Seconds int            `json:"timeSpentSeconds"`
		Comment map[string]any `json:"comment"`
		Note    string         `json:"-"` // never encoded, so never seen
	}
	w := worklogAdd{Started: "2026-09-19T17:00:00.000+0000", Seconds: 21600, Comment: adf(mention("account-a")), Note: "x"}
	mustAllow(t, assertPermitted(http.MethodPost, worklog, worklogQuery, w, on), "worklog as a struct")
	w.Seconds = 0
	mustRefuse(t, assertPermitted(http.MethodPost, worklog, worklogQuery, w, on), "worklog struct with zero seconds")
}
