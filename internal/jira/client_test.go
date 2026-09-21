package jira

import (
	"net/http"
	"testing"
)

// Read-only cannot be "GET only" here, because Jira's search and
// bulk-fetch endpoints are POSTs that read. The rule is an allowlist
// instead, and these tests hold it to that.

func TestGetIsAllowed(t *testing.T) {
	if err := assertReadOnly(http.MethodGet, "/rest/api/3/field"); err != nil {
		t.Fatalf("GET must be allowed: %v", err)
	}
}

func TestReadPostsAreAllowed(t *testing.T) {
	for _, p := range []string{
		"/rest/api/3/search/jql",
		"/rest/api/3/changelog/bulkfetch",
	} {
		if err := assertReadOnly(http.MethodPost, p); err != nil {
			t.Errorf("POST %s is a read and must be allowed: %v", p, err)
		}
	}
}

// The writes the sprint tool will eventually make must not be reachable
// through this client. They get their own named, audited surface.
func TestWritesAreRefused(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodPut, "/rest/api/3/issue/PROJ-1"},          // assign, set points
		{http.MethodPost, "/rest/api/3/issue/PROJ-1/worklog"}, // log work
		{http.MethodDelete, "/rest/api/3/issue/PROJ-1"},
		{http.MethodPost, "/rest/api/3/issue"},
		{http.MethodPost, "/wiki/rest/api/content"}, // publish a page
		{http.MethodPut, "/wiki/rest/api/content/123"},
		{http.MethodPatch, "/rest/api/3/issue/PROJ-1"},
	}
	for _, c := range cases {
		err := assertReadOnly(c.method, c.path)
		if err == nil {
			t.Errorf("%s %s was allowed; it must be refused", c.method, c.path)
			continue
		}
		if _, ok := err.(*WriteAttemptError); !ok {
			t.Errorf("%s %s: got %T, want *WriteAttemptError", c.method, c.path, err)
		}
	}
}

// A POST to something that merely starts like a read endpoint must not
// slip through.
func TestPostToUnknownPathRefused(t *testing.T) {
	if err := assertReadOnly(http.MethodPost, "/rest/api/3/search"); err == nil {
		t.Error("POST to an unlisted path was allowed")
	}
}

func TestSprintNumberFromName(t *testing.T) {
	cases := map[string]int{
		"Sprint 21":          21,
		"ENG Sprint 7   ":    7,
		"Board 2 Sprint 103": 103,
		"no digits":          0,
	}
	for name, want := range cases {
		if got := numberFromName(name); got != want {
			t.Errorf("numberFromName(%q) = %d, want %d", name, got, want)
		}
	}
}

// A project key goes into a JQL string, so anything that is not a plain
// key is stripped rather than escaped.
func TestProjectKeyIsSanitised(t *testing.T) {
	cases := map[string]string{
		"ENG":                  "ENG",
		"ENG_2":                "ENG_2",
		`ENG" OR project = "X`: "ENGORprojectX",
		"ENG; DROP":            "ENGDROP",
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %q, want %q", in, got, want)
		}
	}
}
