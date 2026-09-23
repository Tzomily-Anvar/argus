// Package jira is a client for Jira Cloud that reads anything and writes
// a closed list of things.
//
// Every request passes through one function, do, whose first statement
// is assertPermitted. Reads are decided the way they always were: GET
// always, and POST only to the search and bulk-fetch endpoints, which
// read despite the verb because the query travels in the body. Writes
// are the list in permit.go - the story points field, the assignee, and
// adding, correcting or removing one worklog entry - each pinned to an
// exact path, an exact query and an exact body shape. Even a request on
// that list is refused unless the Client's writePolicy allows writes;
// New leaves it off, and nothing in this codebase yet switches it on.
//
// TestOnlyDoReachesTheNetwork keeps this structural: a file in this
// package that builds its own request fails the build, so a second
// route to the network has to be argued for in review rather than
// slipped in.
//
// Authentication is Basic with an email and an API token, which is what
// Jira Cloud expects - not a bearer token.
package jira

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const userAgent = "argus"

// readPaths are the POST endpoints that only read. Matched as prefixes
// against the request path.
var readPaths = []string{
	"/rest/api/3/search/jql",
	"/rest/api/3/changelog/bulkfetch",
}

// Error wraps any failure talking to Jira, so callers never see a raw
// net/http error.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errf(format string, a ...any) error { return &Error{msg: fmt.Sprintf(format, a...)} }

// WriteAttemptError means something tried to make this client write.
type WriteAttemptError struct{ msg string }

func (e *WriteAttemptError) Error() string { return e.msg }

// Client talks to one Jira site. Safe for concurrent use once set up;
// AllowWrites is part of setting up.
type Client struct {
	http    *http.Client
	baseURL string
	auth    string
	sem     chan struct{}

	// policy is consulted by the gate in do and nowhere else. Its zero
	// value refuses every write.
	policy writePolicy
}

// New returns a client for baseURL, authenticating as email with token.
func New(baseURL, email, token string, concurrency, timeoutSeconds int) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errf("no Jira base URL")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, errf("invalid Jira base URL %q: %v", baseURL, err)
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return &Client{
		http:    &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		baseURL: baseURL,
		auth:    "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token)),
		sem:     make(chan struct{}, concurrency),
	}, nil
}

// BaseURL is the site this client talks to, for building browse links.
func (c *Client) BaseURL() string { return c.baseURL }

// AllowWrites switches on the writes permit.go lists, with pointsField as
// the custom field id that "story points" means on this site.
//
// Nothing calls this yet. It exists so that when a caller arrives it has
// one place to do this, at startup, from the one setting that governs
// it - and so the setting is never consulted at a call site, where it
// could also be forgotten. Call it before the client is shared; it is
// not synchronised with requests in flight.
func (c *Client) AllowWrites(pointsField string) {
	c.policy = writePolicy{Allowed: true, PointsField: pointsField}
}

// do is the one place HTTP happens. Three attempts with backoff on
// transient failures; an explicit HTTP error is final.
//
// The path and the query arrive separately so the gate can judge each
// on its own terms: the path against an exact pattern, the query against
// an exact set of parameters.
func (c *Client) do(method, path, rawQuery string, body any) ([]byte, error) {
	if err := assertPermitted(method, path, rawQuery, body, c.policy); err != nil {
		return nil, err
	}

	var encoded []byte
	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			return nil, errf("encoding request body: %v", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		raw, err, retry := c.attempt(method, path, rawQuery, encoded)
		if err == nil {
			return raw, nil
		}
		if !retry {
			return nil, err
		}
		lastErr = err
		time.Sleep(time.Duration(500*(attempt+1)) * time.Millisecond)
	}
	return nil, errf("network error for %s after 3 attempts: %v", path, lastErr)
}

func (c *Client) attempt(method, path, rawQuery string, encoded []byte) ([]byte, error, bool) {
	var reader io.Reader
	if encoded != nil {
		reader = bytes.NewReader(encoded)
	}
	target := c.baseURL + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, errf("building request: %v", err), false
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if encoded != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.sem <- struct{}{}
	resp, err := c.http.Do(req)
	<-c.sem
	if err != nil {
		return nil, err, true
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err, true
	}
	if resp.StatusCode >= 400 {
		return nil, explain(resp.StatusCode, path, raw), false
	}
	return raw, nil, false
}

// explain turns Jira's terser failures into something actionable. Its 401
// and 404 in particular say very little about which of several setup
// mistakes you actually made.
func explain(status int, path string, raw []byte) error {
	detail := strings.TrimSpace(string(raw))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case 401:
		return errf("Jira rejected the credentials (401). Check ARGUS_JIRA_EMAIL is the " +
			"address you log in with, and that ARGUS_JIRA_TOKEN is a current API token " +
			"from id.atlassian.com/manage-profile/security/api-tokens.")
	case 403:
		return errf("Jira returned 403 for %s. The credentials are valid but this account "+
			"cannot see that resource. Detail: %s", path, detail)
	case 404:
		return errf("Jira returned 404 for %s. Either ARGUS_JIRA_BASE_URL points at the "+
			"wrong site, or the project or sprint does not exist under it.", path)
	case 429:
		return errf("Jira is rate limiting (429). Lower ARGUS_CONCURRENCY and try again.")
	}
	return errf("%d for %s :: %s", status, path, detail)
}

// ---- requests --------------------------------------------------------

// Get performs a GET and decodes the response into v.
func (c *Client) Get(path string, params url.Values, v any) error {
	raw, err := c.do(http.MethodGet, path, params.Encode(), nil)
	if err != nil {
		return err
	}
	return decode(raw, path, v)
}

// Post performs a POST to one of the read endpoints and decodes into v.
func (c *Client) Post(path string, body, v any) error {
	raw, err := c.do(http.MethodPost, path, "", body)
	if err != nil {
		return err
	}
	return decode(raw, path, v)
}

func decode(raw []byte, path string, v any) error {
	if v == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return errf("decoding response from %s: %v", path, err)
	}
	return nil
}

// Agile performs a GET against the Agile API, which is where sprints and
// boards live rather than the platform API.
func (c *Client) Agile(path string, params url.Values, v any) error {
	return c.Get("/rest/agile/1.0"+path, params, v)
}
