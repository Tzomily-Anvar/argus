package gh

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestBucketOfSeparatesTheThreeLimits(t *testing.T) {
	cases := map[string]string{
		apiBase + "/repos/octocat/example/pulls/1": BucketCore,
		apiBase + "/search/issues?q=is%3Apr":       BucketSearch,
		graphqlURL:                                 BucketGraphQL,
	}
	for reqURL, want := range cases {
		method := http.MethodGet
		if want == BucketGraphQL {
			method = http.MethodPost
		}
		if got := bucketOf(method, reqURL, graphqlURL); got != want {
			t.Errorf("bucketOf(%s) = %q, want %q", reqURL, got, want)
		}
	}
}

// The reserve is the whole point of counting: a background tool must not
// spend the last of a token its owner is also using.
func TestReserveStopsBeforeTheBucketIsEmpty(t *testing.T) {
	m := &meter{}
	future := time.Now().Add(time.Hour)

	m.observe(limitHeaders(30, 20, future), BucketSearch)
	if err := m.reserveCheck(BucketSearch); err != nil {
		t.Fatalf("20 of 30 left is plenty, got %v", err)
	}

	m.observe(limitHeaders(30, 3, future), BucketSearch)
	err := m.reserveCheck(BucketSearch)
	if !IsRateLimited(err) {
		t.Fatalf("3 of 30 left is inside the reserve, got %v", err)
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) || !rl.Withheld {
		t.Errorf("a request Argus declined to make must say so: %v", err)
	}
}

// An observation from a window that has already reset says nothing about
// the one we are in now.
func TestReserveIgnoresAnExpiredObservation(t *testing.T) {
	m := &meter{}
	m.observe(limitHeaders(30, 0, time.Now().Add(-time.Minute)), BucketSearch)
	if err := m.reserveCheck(BucketSearch); err != nil {
		t.Fatalf("the window has reset, so the bucket has refilled: %v", err)
	}
}

func TestSecondaryLimitHonoursRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("retry-after", "12")
	wait, ok := secondaryWait(http.StatusForbidden, h, "")
	if !ok || wait != 12*time.Second {
		t.Errorf("retry-after must be obeyed as given, got %v %v", wait, ok)
	}

	wait, ok = secondaryWait(http.StatusForbidden, http.Header{},
		`{"message":"You have exceeded a secondary rate limit"}`)
	if !ok || wait != defaultSecondaryWait {
		t.Errorf("a named secondary limit with no retry-after must still wait, got %v %v", wait, ok)
	}

	if _, ok := secondaryWait(http.StatusForbidden, http.Header{}, `{"message":"Resource not accessible"}`); ok {
		t.Error("an ordinary permission refusal is not a rate limit and must not be retried")
	}
}

// An exhausted primary limit resets up to an hour later, so retrying into
// it is pointless; it must be final, and it must be legible.
func TestExhaustedPrimaryLimitIsFinal(t *testing.T) {
	h := limitHeaders(5000, 0, time.Now().Add(time.Hour))
	h.Set("x-ratelimit-resource", BucketCore)
	rl := primaryExhausted(http.StatusForbidden, h, BucketCore)
	if rl == nil {
		t.Fatal("remaining=0 on a 403 is an exhausted limit")
	}
	if !IsRateLimited(rl) {
		t.Error("it must be recognisable as a rate limit rather than a fault")
	}
}

// A hold applies to the whole client. One goroutine backing off while the
// others keep knocking is not stopping.
func TestHoldDelaysEveryCaller(t *testing.T) {
	m := &meter{}
	m.hold(80 * time.Millisecond)
	start := time.Now()
	m.waitTurn()
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Errorf("waitTurn returned after %v, before the hold expired", elapsed)
	}
}

// 304 costs nothing against the rate limit, which is the single most
// respectful thing a tool sweeping every quarter hour can do.
func TestConditionalRequestReusesTheCachedBody(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `W/"abc"`)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("If-None-Match") == `W/"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte(`{"name":"example","count":1}`))
	}))
	defer srv.Close()

	c := NewForTest(srv.URL, "ghp_x")
	first, err := c.Get("/repos/octocat/example", nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	second, err := c.Get("/repos/octocat/example", nil)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if second["name"] != "example" {
		t.Fatalf("a 304 must yield the cached body, got %v", second)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two round trips, got %d", calls.Load())
	}
	if b := c.Budget(); b.NotModified != 1 {
		t.Errorf("the free response was not counted: %+v", b)
	}

	// Callers write into what they are handed - the per-repository alert
	// feed stamps a repository name onto every alert - so two sweeps must
	// not be given the same map.
	first["name"] = "mutated"
	third, err := c.Get("/repos/octocat/example", nil)
	if err != nil {
		t.Fatalf("third fetch: %v", err)
	}
	if third["name"] != "example" {
		t.Error("a cached response was aliased to an earlier caller and got corrupted")
	}
}

// ARGUS_CONCURRENCY is a number someone types. A mistyped one must not
// turn a background tool into something that hammers a shared API.
func TestConcurrencyIsBounded(t *testing.T) {
	if c := New("ghp_x", 5000, 10); cap(c.sem) != maxConcurrency {
		t.Errorf("concurrency %d was not clamped, got %d", 5000, cap(c.sem))
	}
	if c := New("ghp_x", 0, 10); cap(c.sem) != 1 {
		t.Errorf("concurrency must be at least 1, got %d", cap(c.sem))
	}
}

// The total matters separately from the slice: they differ exactly when
// search hit its ceiling, which is when a broad query stops being a valid
// stand-in for a narrow one.
func TestSearchIssuesReportsTheTotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":4321,"items":[{"number":1}]}`))
	}))
	defer srv.Close()

	items, total, err := NewForTest(srv.URL, "ghp_x").SearchIssues("is:pr is:open")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 1 || total != 4321 {
		t.Errorf("got %d items and a total of %d, want 1 and 4321", len(items), total)
	}
}

func limitHeaders(limit, remaining int, reset time.Time) http.Header {
	h := http.Header{}
	h.Set("x-ratelimit-limit", strconv.Itoa(limit))
	h.Set("x-ratelimit-remaining", strconv.Itoa(remaining))
	h.Set("x-ratelimit-used", strconv.Itoa(limit-remaining))
	h.Set("x-ratelimit-reset", strconv.FormatInt(reset.Unix(), 10))
	return h
}

// An entry count is not a memory bound. This process sits in a small
// container for weeks, so the cache has to be bounded in bytes too.
func TestConditionalCacheIsBoundedInBytes(t *testing.T) {
	var c conditionalCache
	big := make([]byte, maxCachedBytes)
	for i := range 40 {
		c.remember("https://example.test/"+strconv.Itoa(i), `W/"e"`, big, http.Header{})
	}
	if c.cacheBytes > maxCachedTotal {
		t.Errorf("cache grew to %d bytes, over the %d ceiling", c.cacheBytes, maxCachedTotal)
	}
	if _, ok := c.cached("https://example.test/39"); !ok {
		t.Error("the newest entry should survive eviction")
	}
	if _, ok := c.cached("https://example.test/0"); ok {
		t.Error("the oldest entry should have been evicted first")
	}
}

// Exhausting the retries on a rate limit must still report a rate limit.
// The type is how a rule tells "incomplete" from "broken", and wrapping
// it loses that on the one failure where it matters most.
func TestRetriesExhaustedOnALimitStaysARateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("retry-after", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
	}))
	defer srv.Close()

	_, err := NewForTest(srv.URL, "ghp_x").Get("/repos/octocat/example", nil)
	if !IsRateLimited(err) {
		t.Fatalf("got %T %v, want a rate limit", err, err)
	}
}
