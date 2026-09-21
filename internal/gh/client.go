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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/ghauth"
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

// PartialError accompanies data that resolved alongside fields that did
// not. Callers that can work with part of an answer should use the data
// and ignore this; callers that cannot should treat it as a failure.
type PartialError struct{ raw any }

func (e *PartialError) Error() string {
	b, _ := json.Marshal(e.raw)
	msg := string(b)
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return "graphql returned partial data: " + msg
}

// IsPartial reports whether an error is a partial GraphQL answer.
func IsPartial(err error) bool {
	var p *PartialError
	return errors.As(err, &p)
}

// Client talks to GitHub. Safe for concurrent use.
// TokenSource yields a valid token for each request. A personal access
// token is a constant; a GitHub App's user token expires every eight
// hours, so it cannot be captured once at construction.
type TokenSource func(context.Context) (string, error)

type Client struct {
	http      *http.Client
	token     TokenSource
	tokenType TokenType
	sem       chan struct{}

	// Where the API is. Constant in production - the fields exist so a
	// test can point the real request path at an httptest server rather
	// than reimplement it.
	base    string
	graphql string

	accountCache
}

// New returns a client bounded to `concurrency` in-flight requests. The
// bound is shared across every rule sweeping at once, which is what
// keeps us clear of GitHub's secondary rate limits.
func New(token string, concurrency, timeoutSeconds int) *Client {
	if concurrency < 1 {
		concurrency = 1
	}
	return NewWithSource(
		func(context.Context) (string, error) { return token, nil },
		resolveTokenType(token), concurrency, timeoutSeconds)
}

// NewWithSource builds a client whose token is fetched per request.
func NewWithSource(src TokenSource, kind TokenType, concurrency, timeoutSeconds int) *Client {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Client{
		http:      &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		token:     src,
		tokenType: kind,
		sem:       make(chan struct{}, concurrency),
		base:      apiBase,
		graphql:   graphqlURL,
	}
}

// NewForTest returns a client that talks to base instead of GitHub, so a
// test drives the same do() path everything else uses. There is no way
// to redirect a client built by New or NewWithSource, which is the
// point: nothing configurable can send a real sweep somewhere else.
func NewForTest(base, token string) *Client {
	c := New(token, 1, 10)
	c.base = strings.TrimSuffix(base, "/")
	c.graphql = c.base + "/graphql"
	return c
}

// TokenType reports what kind of credential this client holds.
func (c *Client) TokenType() TokenType { return c.tokenType }

// resolveTokenType honours an explicit override and otherwise reads the
// type from the token's prefix. The override exists for tokens issued
// before GitHub used prefixes, and for anything it cannot recognise.
func resolveTokenType(token string) TokenType {
	switch strings.ToLower(config.String("ARGUS_GITHUB_TOKEN_TYPE", "auto")) {
	case "fine-grained", "fine_grained", "finegrained":
		return TokenFineGrained
	case "classic":
		return TokenClassic
	}
	if t := DetectTokenType(token); t != TokenUnknown {
		return t
	}
	// Unrecognised: assume fine-grained, which is the kind people should
	// be creating now, so the advice points at the right screen.
	return TokenFineGrained
}

// NewFromEnv builds a client from whichever credential is configured.
//
// A token that someone has actually set wins. The App's client id ships
// with a default so `argus login` needs no setup, which makes "an App is
// configured" true of every installation - so it cannot also be the
// signal to prefer the App, or everyone already using a token would
// silently lose it on upgrade.
func NewFromEnv() (*Client, error) {
	// 1. A token set for Argus specifically. This is the container's
	//    path, and an explicit choice either way.
	if tok := config.ArgusToken(); tok != "" {
		return New(tok, config.Concurrency(), config.HTTPTimeout()), nil
	}

	// 2. A sign-in someone actually performed. It outranks GH_TOKEN and
	//    GITHUB_TOKEN, which are usually exported for some other tool and
	//    would otherwise quietly override `argus login`.
	if config.UseGitHubApp() {
		src := ghauth.NewSource(config.GitHubAppClientID(), config.DataDir(),
			config.DataDir() == config.DefaultDataDir())
		if src.SignedIn() {
			return NewWithSource(src.Token, TokenAppUser, config.Concurrency(), config.HTTPTimeout()), nil
		}
	}

	// 3. Whatever the environment already had.
	if tok := config.AmbientToken(); tok != "" {
		return New(tok, config.Concurrency(), config.HTTPTimeout()), nil
	}

	return nil, fmt.Errorf(
		"no GitHub credential yet. Run `argus login` to sign in, " +
			"or set ARGUS_GITHUB_TOKEN to a personal access token")
}

// assertReadOnly is given the GraphQL endpoint rather than assuming the
// constant, because a client under test has its own. It stays an exact
// match either way: "a URL that ends in /graphql" would be a weaker
// guarantee than the one this package makes.
func assertReadOnly(method, reqURL, graphqlEndpoint string, body any) error {
	if method == http.MethodGet {
		return nil
	}
	if method == http.MethodPost && reqURL == graphqlEndpoint {
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
	if err := assertReadOnly(method, reqURL, c.graphql, body); err != nil {
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
	tok, err := c.token(context.Background())
	if err != nil {
		return nil, nil, err, false
	}
	req.Header.Set("Authorization", "Bearer "+tok)
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
		return nil, nil, explain(resp.StatusCode, reqURL, raw, c.tokenType), false
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
func explain(status int, reqURL string, raw []byte, tt TokenType) error {
	detail := string(raw)
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch {
	case status == 401:
		return errf("GitHub rejected the %s (401). It is missing, mistyped, expired, or revoked. "+
			"Check ARGUS_GITHUB_TOKEN.", tt.Label())
	case status == 403 && strings.Contains(strings.ToLower(detail), "saml"):
		return errf("GitHub returned 403 with a SAML/SSO error. The token is valid but has not been " +
			"authorised for this organisation. Open the token in GitHub settings and use " +
			"'Configure SSO' -> Authorize. See the token setup section in the README.")
	case status == 403:
		return errf("GitHub returned 403 for %s, using a %s. %s Detail: %s",
			reqURL, tt.Label(), tt.permissionAdvice(), detail)
	case status == 404:
		return errf("GitHub returned 404 for %s. Either ARGUS_GITHUB_ORG names an account that "+
			"does not exist, or your token cannot see it.", reqURL)
	}
	return errf("%d for %s :: %s", status, reqURL, detail)
}

// ---- REST ------------------------------------------------------------

func (c *Client) Get(path string, params url.Values) (map[string]any, error) {
	u := c.base + path
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
	u := c.base + path + "?" + params.Encode()

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
	payload, _, err := c.do(http.MethodPost, c.graphql, map[string]any{
		"query": query, "variables": variables,
	})
	if err != nil {
		return nil, err
	}
	return decodeGraphQL(payload)
}

// decodeGraphQL separates a usable answer from a failed one.
func decodeGraphQL(payload map[string]any) (map[string]any, error) {
	data, _ := payload["data"].(map[string]any)

	// GraphQL answers partially: a field the token cannot read comes back
	// as an error alongside the fields it could. Treating any error as
	// fatal throws away everything that did resolve.
	//
	// That is not hypothetical. A fine-grained token cannot read check
	// runs at all - GitHub grants that only to Apps - so every pull
	// request query returns a FORBIDDEN on the check contexts and useful
	// data beside it. Discarding the lot made the whole rule look empty
	// rather than partially answered.
	if errs, ok := payload["errors"]; ok {
		if len(data) == 0 {
			b, _ := json.Marshal(errs)
			msg := string(b)
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return nil, errf("graphql: %s", msg)
		}
		return data, &PartialError{raw: errs}
	}
	return data, nil
}
