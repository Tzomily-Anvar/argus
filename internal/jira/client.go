// Package jira is a read-only client for Jira Cloud.
//
// Read-only is enforced the same way the GitHub client enforces it, but
// the rule cannot be "GET only": Jira's search and bulk-fetch endpoints
// are POSTs that read, because the query travels in the body. So instead
// of a verb rule there is an allowlist of endpoints known to be reads,
// and anything else is refused before it is sent.
//
// That list is short and explicit on purpose. When the sprint tool gains
// the ability to reassign a ticket or publish a page, those writes will
// be their own named, audited, confirmed surface rather than a quiet
// relaxation of this one.
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

// Client talks to one Jira site. Safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
	auth    string
	sem     chan struct{}
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

func assertReadOnly(method, path string) error {
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
	return &WriteAttemptError{
		msg: fmt.Sprintf("%s %s is not a known read; this client is read-only", method, path),
	}
}

// do is the one place HTTP happens. Three attempts with backoff on
// transient failures; an explicit HTTP error is final.
func (c *Client) do(method, path string, body any) ([]byte, error) {
	if err := assertReadOnly(method, path); err != nil {
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
		raw, err, retry := c.attempt(method, path, encoded)
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

func (c *Client) attempt(method, path string, encoded []byte) ([]byte, error, bool) {
	var reader io.Reader
	if encoded != nil {
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
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
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	raw, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return decode(raw, path, v)
}

// Post performs a POST to one of the read endpoints and decodes into v.
func (c *Client) Post(path string, body, v any) error {
	raw, err := c.do(http.MethodPost, path, body)
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
