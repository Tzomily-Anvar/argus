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
	conditionalCache

	// Named rather than embedded: the meter has a graphql counter and the
	// client has a graphql endpoint, and one shadowing the other is the
	// kind of mistake that compiles.
	spend meter
}

// maxConcurrency is a ceiling on however many in-flight requests someone
// configures.
//
// GitHub's own advice on secondary rate limits is to make requests
// serially. Argus does not - a sweep that fetched thirty repositories one
// after another would take long enough to be useless - but "as many as
// you like" is not the other end of that trade. The ceiling exists so a
// mistyped ARGUS_CONCURRENCY cannot turn a background tool into something
// that hammers a shared API on somebody's behalf.
const maxConcurrency = 32

// New returns a client bounded to `concurrency` in-flight requests. The
// bound is shared across every rule sweeping at once, which is what
// keeps us clear of GitHub's secondary rate limits.
func New(token string, concurrency, timeoutSeconds int) *Client {
	return NewWithSource(
		func(context.Context) (string, error) { return token, nil },
		resolveTokenType(token), concurrency, timeoutSeconds)
}

// NewWithSource builds a client whose token is fetched per request.
func NewWithSource(src TokenSource, kind TokenType, concurrency, timeoutSeconds int) *Client {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > maxConcurrency {
		concurrency = maxConcurrency
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

	// Classified once rather than per attempt: a retried search is still
	// a search, and it is charged to the search bucket either way.
	bucket := bucketOf(method, reqURL, c.graphql)

	// Refuse to dip into the reserve. Cheaper than asking GitHub, and it
	// happens before the request rather than after the damage.
	if err := c.spend.reserveCheck(bucket); err != nil {
		return nil, nil, err
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			c.spend.countRetry()
		}
		c.spend.count(bucket)
		payload, headers, err, wait := c.attempt(method, reqURL, encoded, bucket, attempt)
		if err == nil {
			return payload, headers, nil
		}
		if wait < 0 {
			return nil, nil, err
		}
		lastErr = err
		if attempt == maxAttempts-1 {
			break
		}
		time.Sleep(wait)
	}
	// Returned as-is when it is a rate limit, because the type is how a
	// rule tells "incomplete" from "broken" and wrapping it would lose
	// that distinction on exactly the failure that most needs it.
	if IsRateLimited(lastErr) {
		return nil, nil, lastErr
	}
	return nil, nil, errf("gave up on %s after %d attempts: %v", reqURL, maxAttempts, lastErr)
}

// attempt performs one request. The body is closed before returning, so
// a retry loop cannot accumulate open connections.
//
// The last return value is how long to wait before trying again, or a
// negative duration for "this is final, do not retry". A transient
// network fault backs off gently; a secondary rate limit waits as long as
// GitHub asked and holds the rest of the client back with it; an
// exhausted primary limit or a plain HTTP error does not retry at all.
func (c *Client) attempt(method, reqURL string, encoded []byte, bucket string, attempt int) (map[string]any, http.Header, error, time.Duration) {
	var reader io.Reader
	if encoded != nil {
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, reqURL, reader)
	if err != nil {
		return nil, nil, errf("building request: %v", err), -1
	}
	tok, err := c.token(context.Background())
	if err != nil {
		return nil, nil, err, -1
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	if encoded != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Ask GitHub whether anything has changed. An unchanged resource
	// answers 304 with no body and costs no rate limit.
	var prior cachedResponse
	conditional := method == http.MethodGet
	if conditional {
		if e, ok := c.cached(reqURL); ok {
			prior = e
			req.Header.Set("If-None-Match", e.etag)
		}
	}

	// A secondary rate limit holds the whole client, so honour it before
	// taking a slot rather than while occupying one.
	c.spend.waitTurn()

	c.sem <- struct{}{}
	resp, err := c.http.Do(req)
	<-c.sem
	if err != nil {
		return nil, nil, err, backoff(attempt)
	}
	defer resp.Body.Close()

	// Free information: GitHub states what is left of the bucket this
	// request was charged to, on every response including the failures.
	c.spend.observe(resp.Header, bucket)

	if resp.StatusCode == http.StatusNotModified && prior.raw != nil {
		c.spend.countNotModified()
		// Decoded afresh rather than handed out: callers write into what
		// they are given, and two sweeps must not share one map.
		payload, err := decode(prior.raw, reqURL)
		if err != nil {
			return nil, nil, err, -1
		}
		return payload, prior.headers, nil, -1
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err, backoff(attempt)
	}
	if resp.StatusCode >= 400 {
		if wait, ok := secondaryWait(resp.StatusCode, resp.Header, string(raw)); ok {
			if wait > maxSecondaryWait {
				return nil, nil, &RateLimitError{
					Resource: bucket, Secondary: true,
					msg: fmt.Sprintf("GitHub asked for a %s pause on %s (secondary rate limit), "+
						"which is longer than Argus will hold a sweep open. This result is "+
						"incomplete rather than empty.", wait, reqURL),
				}, -1
			}
			// Hold every other goroutine back too. One of them backing
			// off while the rest keep knocking is not stopping.
			c.spend.hold(wait)
			return nil, nil, &RateLimitError{
				Resource: bucket, Secondary: true,
				msg: fmt.Sprintf("GitHub applied a secondary rate limit on %s; waited %s", reqURL, wait),
			}, wait
		}
		if rl := primaryExhausted(resp.StatusCode, resp.Header, bucket); rl != nil {
			return nil, nil, rl, -1
		}
		return nil, nil, explain(resp.StatusCode, reqURL, raw, c.tokenType), -1
	}

	if conditional {
		c.remember(reqURL, resp.Header.Get("ETag"), raw, resp.Header)
	}

	payload, err := decode(raw, reqURL)
	if err != nil {
		return nil, nil, err, -1
	}
	return payload, resp.Header, nil, -1
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
	// Copied, not used in place. url.Values is a map, and this walks the
	// pages by setting one - so a caller that fans this out across
	// goroutines with the same params, which is exactly what the
	// per-repository security feeds do, would otherwise have several
	// goroutines writing to its map at once.
	copied := url.Values{}
	for k, v := range params {
		copied[k] = append([]string(nil), v...)
	}
	params = copied
	if params.Get("per_page") == "" {
		params.Set("per_page", "100")
	}
	u := c.base + path + "?" + params.Encode()

	var out []any
	for page := 0; u != ""; page++ {
		if page > 0 {
			c.spend.countPage()
		}
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

// backoff is the gentle, linear wait for a transient network fault -
// unchanged from before, and deliberately unrelated to the rate-limit
// waits, which come from GitHub rather than from us.
func backoff(attempt int) time.Duration {
	return time.Duration(500*(attempt+1)) * time.Millisecond
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

// SearchIssues pages the issue search API, returning the results and the
// total GitHub says match.
//
// Capped at ten pages because search returns at most 1000 results anyway,
// so an unbounded loop is just a slow way to reach that ceiling. The
// total is returned rather than inferred from the slice because those two
// numbers differ exactly when the ceiling was hit, and a caller deciding
// whether one broad query can stand in for several narrow ones needs to
// know that before it trusts the answer.
//
// Search is the tightest budget GitHub hands out - thirty requests a
// minute, against five thousand an hour for everything else - so each
// page here is worth roughly a hundred and sixty ordinary requests.
func (c *Client) SearchIssues(q string) ([]any, int, error) {
	var items []any
	total := 0
	for page := 1; page <= searchPageCap; page++ {
		payload, err := c.Get("/search/issues", url.Values{
			"q": {q}, "per_page": {"100"}, "page": {fmt.Sprint(page)},
		})
		if err != nil {
			return nil, 0, err
		}
		if page == 1 {
			total = int(Num(payload["total_count"]))
		}
		batch, _ := payload["items"].([]any)
		items = append(items, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return items, total, nil
}

// SearchCeiling is the most results the issue search API will hand back,
// however many match. Past it a query is answering a different question
// from the one that was asked.
const SearchCeiling = 1000

const searchPageCap = SearchCeiling / 100

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
