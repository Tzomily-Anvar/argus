package gh

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// What a sweep costs.
//
// A tool that runs unattended every quarter of an hour is spending
// somebody's rate limit whether or not it admits to it, and the limits it
// spends are not one number. GitHub meters three separate buckets:
//
//   - core, 5000 requests an hour, for ordinary REST;
//   - search, 30 requests a MINUTE, which is the one that actually bites -
//     a sweep that fans out searches can exhaust it mid-flight and get
//     back partial results that look like real answers;
//   - graphql, 5000 points an hour, where a point is not a request. A
//     query that walks a hundred nodes costs more than one that reads a
//     field, so the call count alone says nothing about the spend.
//
// So the client keeps a tally per bucket, and reads the x-ratelimit-*
// headers GitHub already returns on every response - they cost nothing
// extra, and x-ratelimit-used on the graphql endpoint is the only honest
// source for what a query actually cost.
//
// Deliberately counters and a small map, not a log line per request: the
// point is to be cheap enough that nothing has to think about whether to
// switch it on.

// Bucket names GitHub's rate-limit resources, as the x-ratelimit-resource
// header spells them.
const (
	BucketCore    = "core"
	BucketSearch  = "search"
	BucketGraphQL = "graphql"
)

// RateLimit is the state of one bucket, as of the last response that
// mentioned it.
type RateLimit struct {
	Resource  string    `json:"resource"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Used      int       `json:"used"`
	Reset     time.Time `json:"reset"`
	Observed  time.Time `json:"observed"`
}

// Budget is a snapshot of what this client has spent since it was built,
// plus what GitHub last said was left.
//
// The counters are cumulative and never reset, so a caller that wants the
// cost of one sweep takes a snapshot either side and subtracts. That
// keeps concurrent sweeps from stealing each other's numbers.
type Budget struct {
	REST    int64 `json:"rest"`
	Search  int64 `json:"search"`
	GraphQL int64 `json:"graphql"`

	// Pages counts the requests that were the second or later page of a
	// paginated list. It is a subset of REST, not an addition to it:
	// pagination is the difference between "one list" and "one list, nine
	// times", and that difference does not show up in a call count.
	Pages int64 `json:"pages"`

	// Retries counts attempts that failed transiently and were repeated.
	// They cost rate limit like any other request.
	Retries int64 `json:"retries"`

	// NotModified counts requests GitHub answered 304 to. These are a
	// subset of REST and cost nothing against the rate limit, so the
	// interesting number is REST minus this.
	NotModified int64 `json:"not_modified"`

	Limits map[string]RateLimit `json:"limits,omitempty"`
}

// Total is every request the client has made.
func (b Budget) Total() int64 { return b.REST + b.Search + b.GraphQL }

// Sub returns the cost incurred between an earlier snapshot and this one.
// Limits are taken from the later snapshot, since a limit is a level
// rather than a quantity.
func (b Budget) Sub(earlier Budget) Budget {
	return Budget{
		REST:        b.REST - earlier.REST,
		Search:      b.Search - earlier.Search,
		GraphQL:     b.GraphQL - earlier.GraphQL,
		Pages:       b.Pages - earlier.Pages,
		Retries:     b.Retries - earlier.Retries,
		NotModified: b.NotModified - earlier.NotModified,
		Limits:      b.Limits,
	}
}

// GraphQLPointsSince returns the graphql points spent between two
// snapshots, and whether GitHub reported enough to tell.
//
// Points are the only bucket where the call count is meaningless, and
// they are not derivable from anything Argus can see locally - the number
// comes from GitHub or not at all.
func (b Budget) GraphQLPointsSince(earlier Budget) (int, bool) {
	now, ok := b.Limits[BucketGraphQL]
	if !ok {
		return 0, false
	}
	before, ok := earlier.Limits[BucketGraphQL]
	if !ok || before.Reset != now.Reset {
		// A reset in between means Used went backwards; the difference
		// would be a negative number pretending to be a measurement.
		return 0, false
	}
	return now.Used - before.Used, true
}

// meter is embedded in Client.
type meter struct {
	rest    atomic.Int64
	search  atomic.Int64
	graphql atomic.Int64
	pages   atomic.Int64
	retries atomic.Int64
	fresh   atomic.Int64

	limitMu sync.Mutex
	limits  map[string]RateLimit
	// pausedUntil is when a secondary rate limit says the client may
	// speak again. Guarded by limitMu because it is set from the same
	// place the headers are read.
	pausedUntil time.Time
}

// bucket classifies a request the way GitHub meters it. Done once per
// call in do() rather than per attempt, since a retry of a search is
// still a search.
func bucketOf(method, reqURL, graphqlEndpoint string) string {
	if method == http.MethodPost && reqURL == graphqlEndpoint {
		return BucketGraphQL
	}
	// Every search endpoint lives under /search/, and nothing else does.
	if strings.Contains(reqURL, "/search/") {
		return BucketSearch
	}
	return BucketCore
}

func (m *meter) count(bucket string) {
	switch bucket {
	case BucketGraphQL:
		m.graphql.Add(1)
	case BucketSearch:
		m.search.Add(1)
	default:
		m.rest.Add(1)
	}
}

func (m *meter) countPage()        { m.pages.Add(1) }
func (m *meter) countRetry()       { m.retries.Add(1) }
func (m *meter) countNotModified() { m.fresh.Add(1) }

// observe records the rate-limit headers. GitHub sends them on every
// response including errors, so this is free information; a response
// without them - a test server, say - is simply not recorded.
func (m *meter) observe(h http.Header, fallback string) {
	if h == nil {
		return
	}
	limit, ok := atoi(h.Get("x-ratelimit-limit"))
	if !ok {
		return
	}
	resource := h.Get("x-ratelimit-resource")
	if resource == "" {
		resource = fallback
	}
	remaining, _ := atoi(h.Get("x-ratelimit-remaining"))
	used, _ := atoi(h.Get("x-ratelimit-used"))
	var reset time.Time
	if sec, ok := atoi(h.Get("x-ratelimit-reset")); ok {
		reset = time.Unix(int64(sec), 0).UTC()
	}

	m.limitMu.Lock()
	defer m.limitMu.Unlock()
	if m.limits == nil {
		m.limits = map[string]RateLimit{}
	}
	m.limits[resource] = RateLimit{
		Resource:  resource,
		Limit:     limit,
		Remaining: remaining,
		Used:      used,
		Reset:     reset,
		Observed:  time.Now().UTC(),
	}
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Budget reports what this client has spent so far.
func (c *Client) Budget() Budget {
	m := &c.spend
	b := Budget{
		REST:        m.rest.Load(),
		Search:      m.search.Load(),
		GraphQL:     m.graphql.Load(),
		Pages:       m.pages.Load(),
		Retries:     m.retries.Load(),
		NotModified: m.fresh.Load(),
	}
	m.limitMu.Lock()
	defer m.limitMu.Unlock()
	if len(m.limits) > 0 {
		b.Limits = make(map[string]RateLimit, len(m.limits))
		for k, v := range m.limits {
			b.Limits[k] = v
		}
	}
	return b
}

// Summary renders a budget as one line, for whoever is running Argus to
// see what it costs them.
//
// One line per sweep, not one per request: the question a reader has is
// "what did that cost", and a log they have to add up themselves does not
// answer it.
func (b Budget) Summary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d requests (rest %d, search %d, graphql %d",
		b.Total(), b.REST, b.Search, b.GraphQL)
	if b.Pages > 0 {
		fmt.Fprintf(&sb, "; %d were extra pages", b.Pages)
	}
	if b.NotModified > 0 {
		fmt.Fprintf(&sb, "; %d unchanged and free", b.NotModified)
	}
	if b.Retries > 0 {
		fmt.Fprintf(&sb, "; %d retried", b.Retries)
	}
	sb.WriteString(")")
	for _, name := range []string{BucketCore, BucketSearch, BucketGraphQL} {
		l, ok := b.Limits[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, ", %s %d/%d left", name, l.Remaining, l.Limit)
	}
	return sb.String()
}
