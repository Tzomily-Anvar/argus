package jira

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Reads cannot be "GET only" here, because Jira's search and bulk-fetch
// endpoints are POSTs that read. The rule is an allowlist instead, and
// these tests hold it to that with the zero policy - the one New hands
// out, under which nothing may be written.

var off writePolicy

func TestGetIsAllowed(t *testing.T) {
	for _, p := range []string{"/rest/api/3/field", "/rest/agile/1.0/sprint/7", "/rest/api/3/issue/ABC-123/worklog"} {
		if err := assertPermitted(http.MethodGet, p, "accountId=abc", nil, off); err != nil {
			t.Errorf("GET %s must be allowed: %v", p, err)
		}
	}
}

func TestReadPostsAreAllowed(t *testing.T) {
	for _, p := range []string{
		"/rest/api/3/search/jql",
		"/rest/api/3/changelog/bulkfetch",
	} {
		if err := assertPermitted(http.MethodPost, p, "", map[string]any{"jql": "x"}, off); err != nil {
			t.Errorf("POST %s is a read and must be allowed: %v", p, err)
		}
	}
}

// With the zero policy nothing is written. What differs is why: a request
// on the permitted list is refused because writes are off, and the
// operator can change that; anything else is refused because it was
// never on the list, and no setting will ever let it through.
func TestWritesAreRefused(t *testing.T) {
	points := map[string]any{"fields": map[string]any{"customfield_10000": 3.0}}
	cases := []struct {
		method, path string
		body         any
		disabled     bool // refused by the setting rather than by the list
	}{
		{http.MethodPut, "/rest/api/3/issue/ABC-1", points, true},                    // set points
		{http.MethodPost, "/rest/api/3/issue/ABC-1/worklog", map[string]any{}, true}, // log work
		{http.MethodDelete, "/rest/api/3/issue/ABC-1/worklog/45", nil, true},
		{http.MethodDelete, "/rest/api/3/issue/ABC-1", nil, false},
		{http.MethodPost, "/rest/api/3/issue", points, false},
		{http.MethodPost, "/wiki/rest/api/content", nil, false}, // publish a page
		{http.MethodPut, "/wiki/rest/api/content/123", nil, false},
		{http.MethodPatch, "/rest/api/3/issue/ABC-1", points, false},
	}
	for _, c := range cases {
		err := assertPermitted(c.method, c.path, "", c.body, off)
		if err == nil {
			t.Errorf("%s %s was allowed; it must be refused", c.method, c.path)
			continue
		}
		var disabled *WritesDisabledError
		var attempt *WriteAttemptError
		switch {
		case c.disabled && !errors.As(err, &disabled):
			t.Errorf("%s %s: got %T, want *WritesDisabledError", c.method, c.path, err)
		case !c.disabled && !errors.As(err, &attempt):
			t.Errorf("%s %s: got %T, want *WriteAttemptError", c.method, c.path, err)
		}
	}
}

// A POST to something that merely starts like a read endpoint must not
// slip through.
func TestPostToUnknownPathRefused(t *testing.T) {
	if err := assertPermitted(http.MethodPost, "/rest/api/3/search", "", nil, off); err == nil {
		t.Error("POST to an unlisted path was allowed")
	}
}

// A client from New refuses a write before anything is sent. The server
// here fails the test if it is reached at all.
func TestNewClientRefusesWritesBeforeTheNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the network was reached: %s %s", r.Method, r.URL)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "person.a@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"fields": map[string]any{"customfield_10000": 3.0}}
	_, err = c.do(http.MethodPut, "/rest/api/3/issue/ABC-123", "", body)
	var disabled *WritesDisabledError
	if !errors.As(err, &disabled) {
		t.Fatalf("got %v, want *WritesDisabledError", err)
	}
	if !strings.Contains(err.Error(), "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Errorf("the refusal must name the setting: %q", err)
	}
}

// Once writes are allowed, what the gate passed is what goes on the wire:
// the path and query the gate saw separately are reassembled, and the
// body is the same encoding the gate judged.
func TestDoSendsWhatTheGatePassed(t *testing.T) {
	type seen struct {
		method, path, query string
		body                []byte
	}
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = seen{r.Method, r.URL.Path, r.URL.RawQuery, b}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "person.a@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	c.AllowWrites("customfield_10000")

	body := map[string]any{
		"started":          "2026-09-19T17:00:00.000+0000",
		"timeSpentSeconds": 3600,
		"comment":          adf(mention("account-a")),
	}
	if _, err := c.do(http.MethodPost, "/rest/api/3/issue/ABC-123/worklog", worklogQuery, body); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(body)
	if got.method != http.MethodPost || got.path != "/rest/api/3/issue/ABC-123/worklog" ||
		got.query != worklogQuery || !bytes.Equal(got.body, want) {
		t.Errorf("sent %+v", got)
	}

	// And a malformed body is still refused, writes on or not.
	_, err = c.do(http.MethodPut, "/rest/api/3/issue/ABC-123", "", map[string]any{"fields": map[string]any{}})
	var attempt *WriteAttemptError
	if !errors.As(err, &attempt) {
		t.Errorf("an empty edit was not refused: %v", err)
	}
}

// A structural test, in the spirit of TestEverySettingIsDocumented. The
// guarantee that every write meets the gate is a property of the package,
// not of one function, and it only holds while the gated files are the
// only way out to the network.
//
// This is a smell detector rather than a proof: it reads text, and text
// can be evaded. Its value is that a change adding a second route to the
// network fails CI and has to be argued for in review, which is exactly
// the conversation that should happen.
func TestOnlyDoReachesTheNetwork(t *testing.T) {
	// Every file that may reach the network, and the gate its requests
	// must pass. The teams client talks to a different host with its own
	// read-only allowlist; it is named here so that it stays deliberate.
	gated := map[string]string{
		"client.go": "assertPermitted(",
		"teams.go":  "assertTeamsRead(",
	}
	forbidden := []string{
		"http.NewRequest", "http.Post", "http.Get", "http.DefaultClient",
		"http.Client{", "http.Transport", ".Do(",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if gate, ok := gated[f]; ok {
			if !bytes.Contains(src, []byte(gate)) {
				t.Errorf("%s may reach the network but no longer calls %s", f, gate)
			}
			continue
		}
		for _, bad := range forbidden {
			if bytes.Contains(src, []byte(bad)) {
				t.Errorf("%s uses %s directly. Every Jira request goes through do() in "+
					"client.go, which is where assertPermitted lives.", f, bad)
			}
		}
	}

	// And in client.go the gate is the first thing do does, so nothing
	// can be encoded, retried or sent before it has been judged.
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	first := regexp.MustCompile(`(?s)func \(c \*Client\) do\([^{]*\{\s*if err := assertPermitted\(`)
	if !first.Match(src) {
		t.Error("the first statement of do() in client.go must be the assertPermitted call")
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
