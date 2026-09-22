package gh

import (
	"net/http"
	"sync"
)

// Conditional requests.
//
// Argus sweeps the same URLs every quarter of an hour and most of what it
// asks for has not changed since the last time. GitHub will say so for
// free: send back the ETag it gave you as If-None-Match and an unchanged
// resource answers 304 with no body, which does not count against the
// rate limit at all.
//
// That makes it the cheapest form of good manners available here, and it
// matters most where the request count is highest - a personal account
// has no organisation-wide alert feed, so its security alerts are read
// one repository at a time, and on the second sweep most of those become
// free.
//
// Two decisions worth stating:
//
//   - The raw bytes are cached, not the decoded response. Decoded JSON is
//     a map, callers write into it - the per-repository alert feed stamps
//     the repository name onto every alert - and handing the same map to
//     two sweeps would be both a data race and a corruption. Re-decoding
//     costs microseconds and hands back something nobody else holds.
//
//   - GraphQL is not cached. It is a POST and GitHub does not offer
//     ETags for it, so there is nothing to be conditional about.

const (
	// Enough for a large account's per-repository fan-out, small enough
	// that a long-running process cannot quietly grow without bound.
	maxCachedResponses = 1024
	// A single response larger than this is not worth holding: the point
	// is to save requests, not to become a document store.
	maxCachedBytes = 4 << 20
	// And a ceiling on the lot. An entry count alone is not a memory
	// bound - a thousand four-megabyte pages is four gigabytes - and this
	// process is meant to sit in a small container for weeks.
	maxCachedTotal = 64 << 20
)

type cachedResponse struct {
	etag    string
	raw     []byte
	headers http.Header
}

// conditionalCache is embedded in Client.
type conditionalCache struct {
	cacheMu sync.Mutex
	cache   map[string]cachedResponse
	// Insertion order, so a full cache evicts the oldest entry rather
	// than an arbitrary one. A sweep repeats the same URLs, so evicting
	// at random would throw away the entries most likely to be reused.
	cacheOrder []string
	cacheBytes int
}

func (c *conditionalCache) cached(reqURL string) (cachedResponse, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	e, ok := c.cache[reqURL]
	return e, ok
}

func (c *conditionalCache) remember(reqURL, etag string, raw []byte, headers http.Header) {
	if etag == "" || len(raw) > maxCachedBytes {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.cache == nil {
		c.cache = map[string]cachedResponse{}
	}
	if old, existing := c.cache[reqURL]; existing {
		c.cacheBytes -= len(old.raw)
	} else {
		c.cacheOrder = append(c.cacheOrder, reqURL)
	}
	c.cache[reqURL] = cachedResponse{etag: etag, raw: raw, headers: headers.Clone()}
	c.cacheBytes += len(raw)

	for len(c.cacheOrder) > maxCachedResponses || c.cacheBytes > maxCachedTotal {
		oldest := c.cacheOrder[0]
		c.cacheOrder = c.cacheOrder[1:]
		c.cacheBytes -= len(c.cache[oldest].raw)
		delete(c.cache, oldest)
	}
}
