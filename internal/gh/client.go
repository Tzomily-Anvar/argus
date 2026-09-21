// Package gh is a read-only GitHub client.
//
// Argus never writes to GitHub. It does not comment, approve, label,
// merge, close, or open anything. That is not a convention here, it is
// enforced in one place - do() is the only function that performs HTTP,
// and it rejects anything that is not a read:
//
//   - REST is restricted to GET.
//   - GraphQL needs POST (the document travels in the body), so POST is
//     allowed only to the GraphQL endpoint, and only for a document that
//     begins with "query". A "mutation" is refused before it is sent.
//
// client_test.go asserts both, so a change that tries to make Argus
// write fails the test suite instead of surprising someone's repository.
//
// Requests authenticate with the caller's own token, so results are
// always scoped to what that person can see.
package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
)

const (
	apiBase     = "https://api.github.com"
	graphqlURL  = "https://api.github.com/graphql"
	userAgent   = "argus"
	maxAttempts = 3
)

// Error wraps any failure talking to GitHub. Callers only ever see this
// type, never a raw net/http error.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errf(format string, a ...any) error { return &Error{msg: fmt.Sprintf(format, a...)} }

// WriteAttemptError means something tried to make Argus write. See the
// package doc.
type WriteAttemptError struct{ msg string }

func (e *WriteAttemptError) Error() string { return e.msg }

// Client talks to GitHub. Safe for concurrent use.
type Client struct {
	http  *http.Client
	token string
	sem   chan struct{}
}

// New returns a client bounded to `concurrency` in-flight requests. The
// bound is shared across every rule sweeping at once, which is what
// keeps us clear of GitHub's secondary rate limits.
func New(token string, concurrency, timeoutSeconds int) *Client {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Client{
		http:  &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		token: token,
		sem:   make(chan struct{}, concurrency),
	}
}

// NewFromEnv builds a client from configuration.
func NewFromEnv() (*Client, error) {
	tok, err := config.Token()
	if err != nil {
		return nil, err
	}
	return New(tok, config.Concurrency(), config.HTTPTimeout()), nil
}

func assertReadOnly(method, reqURL string, body any) error {
	if method == http.MethodGet {
		return nil
	}
	if method == http.MethodPost && reqURL == graphqlURL {
		m, _ := body.(map[string]any)
		q, _ := m["query"].(string)
		switch t := strings.TrimSpace(stripLeadingComments(q)); {
		case strings.HasPrefix(t, "query"), strings.HasPrefix(t, "{"):
			return nil
		}
		return &WriteAttemptError{msg: "GraphQL document is not a query; Argus is read-only"}
	}
	return &WriteAttemptError{msg: fmt.Sprintf("%s %s is a write; Argus is read-only", method, reqURL)}
}

func stripLeadingComments(q string) string {
	for {
		q = strings.TrimLeft(q, " \t\r\n")
		if !strings.HasPrefix(q, "#") {
			return q
		}
		if i := strings.IndexByte(q, '\n'); i >= 0 {
			q = q[i+1:]
			continue
		}
		return ""
	}
}

// do is the one place HTTP happens. Three attempts with backoff on
// transient network failures; an explicit HTTP error response is final
// and returned immediately.
func (c *Client) do(method, reqURL string, body any) (map[string]any, http.Header, error) {
	if err := assertReadOnly(method, reqURL, body); err != nil {
		return nil, nil, err
	}

	var encoded []byte
	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			return nil, nil, errf("encoding request body: %v", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		payload, headers, err, retry := c.attempt(method, reqURL, encoded)
		if err == nil {
			return payload, headers, nil
		}
		if !retry {
			return nil, nil, err
		}
		lastErr = err
		time.Sleep(time.Duration(500*(attempt+1)) * time.Millisecond)
	}
	return nil, nil, errf("network error for %s after %d attempts: %v", reqURL, maxAttempts, lastErr)
}

// attempt performs one request. The body is closed before returning, so
// a retry loop cannot accumulate open connections.
func (c *Client) attempt(method, reqURL string, encoded []byte) (map[string]any, http.Header, error, bool) {
	var reader io.Reader
	if encoded != nil {
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, reqURL, reader)
	if err != nil {
		return nil, nil, errf("building request: %v", err), false
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	if encoded != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.sem <- struct{}{}
	resp, err := c.http.Do(req)
	<-c.sem
	if err != nil {
		return nil, nil, err, true
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err, true
	}
	if resp.StatusCode >= 400 {
		return nil, nil, explain(resp.StatusCode, reqURL, raw), false
	}

	payload, err := decode(raw, reqURL)
	if err != nil {
		return nil, nil, err, false
	}
	return payload, resp.Header, nil, false
}

// decode normalises GitHub's two response shapes. List endpoints return
// a bare JSON array; wrapping it as {"_list": [...]} gives callers one
// shape to handle.
func decode(raw []byte, reqURL string) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		var list []any
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, errf("decoding response from %s: %v", reqURL, err)
		}
		return map[string]any{"_list": list}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errf("decoding response from %s: %v", reqURL, err)
	}
	return payload, nil
}

// explain turns GitHub's terser failures into something actionable.
// 401 and 403 here are nearly always one of three setup problems, and
// the raw response says so only obliquely, so name them.
func explain(status int, reqURL string, raw []byte) error {
	detail := string(raw)
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch {
	case status == 401:
		return errf("GitHub rejected the token (401). It is missing, mistyped, expired, or revoked. " +
			"Check ARGUS_GITHUB_TOKEN, or re-run `gh auth login`.")
	case status == 403 && strings.Contains(strings.ToLower(detail), "saml"):
		return errf("GitHub returned 403 with a SAML/SSO error. The token is valid but has not been " +
			"authorised for this organisation. Open the token in GitHub settings and use " +
			"'Configure SSO' -> Authorize. See the token setup section in the README.")
	case status == 403:
		return errf("GitHub returned 403 for %s. Usually a missing scope (repo / read:org / "+
			"security_events) or a rate limit. Detail: %s", reqURL, detail)
	case status == 404:
		return errf("GitHub returned 404 for %s. Either ARGUS_GITHUB_ORG names an organisation that "+
			"does not exist, or your token cannot see it.", reqURL)
	}
	return errf("%d for %s :: %s", status, reqURL, detail)
}

// ---- REST ------------------------------------------------------------

func (c *Client) Get(path string, params url.Values) (map[string]any, error) {
	u := apiBase + path
	if params != nil {
		u += "?" + params.Encode()
	}
	payload, _, err := c.do(http.MethodGet, u, nil)
	return payload, err
}

// GetAll follows Link-header pagination and concatenates every page.
//
// Deliberately sequential. Some org-wide endpoints - dependabot/alerts
// most notably - paginate with an opaque cursor: Link carries rel="next"
// with an `after=` token, there is no rel="last", and ?page=N is
// rejected outright. The next URL is unknowable until the current
// response arrives, so these pages cannot be fetched in parallel.
func (c *Client) GetAll(path string, params url.Values) ([]any, error) {
	if params == nil {
		params = url.Values{}
	}
	if params.Get("per_page") == "" {
		params.Set("per_page", "100")
	}
	u := apiBase + path + "?" + params.Encode()

	var out []any
	for u != "" {
		payload, headers, err := c.do(http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if l, ok := payload["_list"].([]any); ok {
			out = append(out, l...)
		} else if payload != nil {
			out = append(out, payload)
		}
		u = nextLink(headers.Get("Link"))
	}
	return out, nil
}

func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		seg := strings.Split(part, ";")
		if len(seg) >= 2 && strings.Contains(seg[1], `rel="next"`) {
			return strings.Trim(strings.TrimSpace(seg[0]), "<>")
		}
	}
	return ""
}

// SearchIssues pages the issue search API. Capped because search returns
// at most 1000 results anyway, so an unbounded loop is just a slow way
// to reach that ceiling.
func (c *Client) SearchIssues(q string) ([]any, error) {
	var items []any
	for page := 1; page <= 10; page++ {
		payload, err := c.Get("/search/issues", url.Values{
			"q": {q}, "per_page": {"100"}, "page": {fmt.Sprint(page)},
		})
		if err != nil {
			return nil, err
		}
		batch, _ := payload["items"].([]any)
		items = append(items, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return items, nil
}

// ---- GraphQL ---------------------------------------------------------

func (c *Client) GraphQL(query string, variables map[string]any) (map[string]any, error) {
	payload, _, err := c.do(http.MethodPost, graphqlURL, map[string]any{
		"query": query, "variables": variables,
	})
	if err != nil {
		return nil, err
	}
	if errs, ok := payload["errors"]; ok {
		b, _ := json.Marshal(errs)
		msg := string(b)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, errf("graphql: %s", msg)
	}
	data, _ := payload["data"].(map[string]any)
	return data, nil
}
