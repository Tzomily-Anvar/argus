package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// writePolicy is this deployment's answer to "may this client write at
// all", plus the site-specific field id that "points" resolves to.
//
// It is held on the Client and passed into the check rather than read
// from the environment inside it, so the rule stays a pure function of
// its arguments and one test can state the whole of it. The zero value
// permits nothing, and the zero value is what New hands out.
type writePolicy struct {
	Allowed     bool   // ARGUS_SPRINT_ALLOW_WRITES, read once at startup
	PointsField string // customfield_NNNNN for this site, resolved at startup
}

// WritesDisabledError means the request was a permitted operation, but
// this deployment has not switched writes on. It is distinct from
// WriteAttemptError because the two need different answers: one is a
// setting, the other is a bug.
type WritesDisabledError struct{ msg string }

func (e *WritesDisabledError) Error() string { return e.msg }

func refused(format string, a ...any) error {
	return &WriteAttemptError{msg: fmt.Sprintf(format, a...)}
}

// permittedWrite is one entry on the list of writes this client can make.
// Method and Path pick the entry; Query and Body then have to hold before
// the request leaves the process. Body sees the request as Jira would -
// the encoded JSON, not the Go value it came from - so a struct and a map
// that serialise alike are judged alike, by the names on the wire.
type permittedWrite struct {
	Op     string
	Method string
	Path   *regexp.Regexp
	Query  string                                  // the exact query the request must carry; "" for none
	Body   func(raw []byte, pol writePolicy) error // raw is nil when the request has no body
}

// issuePath matches /rest/api/3/issue/ABC-123 and nothing beneath it, so
// .../worklog, .../transitions and .../comment are all outside the field
// edits. Only an issue key, never a numeric id: the key is what the
// preview shows the operator, and the request should name the same thing.
var (
	issuePath        = regexp.MustCompile(`^/rest/api/3/issue/[A-Z][A-Z0-9_]+-[0-9]+$`)
	worklogPath      = regexp.MustCompile(`^/rest/api/3/issue/[A-Z][A-Z0-9_]+-[0-9]+/worklog$`)
	worklogEntryPath = regexp.MustCompile(`^/rest/api/3/issue/[A-Z][A-Z0-9_]+-[0-9]+/worklog/[0-9]+$`)
)

// worklogQuery is the query every worklog write must carry. notifyUsers
// off, because a sprint close that emails the team once per entry is how
// a tool gets banned; adjustEstimate left alone, because Jira's remaining
// estimate is not this tool's business and the default would quietly
// change a field nobody asked about.
const worklogQuery = "notifyUsers=false&adjustEstimate=leave"

// permitted is the entire set of writes this client can make. A list
// rather than a rule: every generalisation here is a field somebody's
// team did not agree to have edited.
//
// Two entries share PUT on an issue, because Jira edits every field
// through the same request. The body says which of the two a request
// is, and it has to be exactly one of them.
var permitted = []permittedWrite{
	{Op: "points.set", Method: http.MethodPut, Path: issuePath, Body: pointsBody},
	{Op: "assignee.set", Method: http.MethodPut, Path: issuePath, Body: assigneeBody},
	{Op: "worklog.add", Method: http.MethodPost, Path: worklogPath, Query: worklogQuery, Body: worklogAddBody},
	{Op: "worklog.update", Method: http.MethodPut, Path: worklogEntryPath, Query: worklogQuery, Body: worklogUpdateBody},
	{Op: "worklog.delete", Method: http.MethodDelete, Path: worklogEntryPath, Query: worklogQuery, Body: noBody},
}

// assertPermitted is the single gate every Jira request passes through.
//
// Reads are decided as they were when this client was read-only: GET
// always, and POST only to the endpoints known to read despite the verb.
// Anything else is a write. A write must match an entry on the permitted
// list by method and exact path, carry that entry's query and no other,
// and carry a body the entry accepts. The list is consulted before the
// policy, so a request nobody ever agreed to is a WriteAttemptError
// whether writes are on or off: that is a bug in the caller, not a
// setting the operator can change. Everything is refused before it
// leaves the process.
func assertPermitted(method, path, rawQuery string, body any, pol writePolicy) error {
	if method == http.MethodGet {
		return nil
	}
	if method == http.MethodPost {
		for _, p := range readPaths {
			if strings.HasPrefix(path, p) {
				return nil
			}
		}
	}

	// A query folded into the path would be matched by the path pattern
	// and never by the query check, so it is not a path.
	if strings.ContainsAny(path, "?#") {
		return refused("%s %s: the path must not carry a query", method, path)
	}
	var candidates []permittedWrite
	for _, w := range permitted {
		if w.Method == method && w.Path.MatchString(path) {
			candidates = append(candidates, w)
		}
	}
	if len(candidates) == 0 {
		return refused("%s %s is not on the list of permitted writes", method, path)
	}
	if !pol.Allowed {
		return &WritesDisabledError{msg: fmt.Sprintf("%s %s is a write, and writes are off for this "+
			"deployment; set ARGUS_SPRINT_ALLOW_WRITES to switch them on", method, path)}
	}

	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return refused("%s %s: the body cannot be encoded: %v", method, path, err)
		}
	}
	var reasons []string
	for _, w := range candidates {
		err := w.Body(raw, pol)
		if !sameQuery(rawQuery, w.Query) {
			err = fmt.Errorf("the query must be exactly %q, not %q", w.Query, rawQuery)
		}
		if err == nil {
			return nil
		}
		reasons = append(reasons, w.Op+": "+err.Error())
	}
	return refused("%s %s refused; %s", method, path, strings.Join(reasons, "; "))
}

// sameQuery reports whether raw carries exactly the parameters in want:
// the same keys, each once, with the same values. Order is forgiven,
// because url.Values encodes in sorted order and a caller building the
// query that way is doing nothing wrong. Nothing else is forgiven: a
// parameter the list does not name is a parameter nobody reviewed.
func sameQuery(raw, want string) bool {
	got, err := url.ParseQuery(raw)
	if err != nil {
		return false
	}
	exp, err := url.ParseQuery(want)
	if err != nil || len(got) != len(exp) {
		return false
	}
	for k, v := range exp {
		if len(got[k]) != 1 || len(v) != 1 || got[k][0] != v[0] {
			return false
		}
	}
	return true
}

// ---- body shapes ------------------------------------------------------

// object decodes raw as a JSON object with its values left encoded, so a
// check can look at exactly the keys it cares about and refuse the rest
// by name. A body that is not an object cannot be any write on the list.
func object(raw []byte) (map[string]json.RawMessage, error) {
	if raw == nil {
		return nil, errors.New("the request has no body")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, errors.New("the body is not a JSON object")
	}
	return m, nil
}

// singleField returns the one field an issue edit sets. Both field edits
// have this shape: the whole body is {"fields": {...}} with exactly one
// key inside. No "update", no "transition", no "properties", no
// "historyMetadata" - each is a different edit with different
// consequences - and {"fields": {}} is refused too, because an empty edit
// is a caller that has lost track of what it is doing.
func singleField(raw []byte) (name string, value json.RawMessage, err error) {
	top, err := object(raw)
	if err != nil {
		return "", nil, err
	}
	if len(top) != 1 || top["fields"] == nil {
		return "", nil, errors.New(`the body must be exactly {"fields": {...}}`)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(top["fields"], &fields); err != nil || fields == nil {
		return "", nil, errors.New("fields is not a JSON object")
	}
	if len(fields) != 1 {
		return "", nil, fmt.Errorf("fields must set exactly one field, not %d", len(fields))
	}
	for k, v := range fields {
		name, value = k, v
	}
	return name, value, nil
}

// pointsBody accepts {"fields": {"<points field>": <number>}} and nothing
// else. The field id comes from the policy rather than the body, because
// the same body against another site's id would edit whatever that field
// happens to be there.
func pointsBody(raw []byte, pol writePolicy) error {
	name, value, err := singleField(raw)
	if err != nil {
		return err
	}
	if pol.PointsField == "" {
		return errors.New("no story points field is configured")
	}
	if name != pol.PointsField {
		return fmt.Errorf("%s is not the story points field", name)
	}
	if _, ok := number(value); !ok {
		return errors.New("story points must be a JSON number")
	}
	return nil
}

// assigneeBody accepts {"fields": {"assignee": {"accountId": "..."}}} and
// nothing else. An account id rather than a name, because names are not
// unique and Jira would guess.
func assigneeBody(raw []byte, _ writePolicy) error {
	name, value, err := singleField(raw)
	if err != nil {
		return err
	}
	if name != "assignee" {
		return fmt.Errorf("%s is not the assignee field", name)
	}
	var who map[string]json.RawMessage
	if err := json.Unmarshal(value, &who); err != nil || len(who) != 1 || who["accountId"] == nil {
		return errors.New(`assignee must be exactly {"accountId": "..."}`)
	}
	if err := nonEmptyString(who["accountId"]); err != nil {
		return fmt.Errorf("assignee accountId %v", err)
	}
	return nil
}

// noBody is the delete: the path names the entry and there is nothing
// else to say. A body on a delete is a caller confused about what it is
// sending.
func noBody(raw []byte, _ writePolicy) error {
	if raw != nil {
		return errors.New("a delete carries no body")
	}
	return nil
}

func number(raw json.RawMessage) (float64, bool) {
	var n *float64 // a pointer, so that JSON null is not read as zero
	if err := json.Unmarshal(raw, &n); err != nil || n == nil {
		return 0, false
	}
	return *n, true
}

func nonEmptyString(raw json.RawMessage) error {
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil || s == nil || *s == "" {
		return errors.New("must be a non-empty string")
	}
	return nil
}
