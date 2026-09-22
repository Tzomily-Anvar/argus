package gh

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Behaving well against somebody else's API.
//
// Argus runs unattended, every quarter of an hour, against a token its
// owner is also using from the `gh` CLI, their editor and their scripts.
// Two things follow from that, and neither is about speed:
//
//   - When GitHub says stop, stop. A secondary rate limit arrives as a
//     403 or 429 carrying retry-after, or a body that names it. Retrying
//     straight into one is precisely the behaviour the limit exists to
//     punish, so the wait is honoured, and it is honoured by the whole
//     client at once - there is no point in one goroutine backing off
//     while nine others keep knocking.
//
//   - Do not spend the last of it. A sweep that drains the hourly
//     allowance leaves its owner unable to use their own token, for a
//     dashboard nobody asked to refresh. So a reserve is kept back, and a
//     sweep that would dip into it stops and says so.
//
// Stopping has to be loud. A rule that quietly returns an empty list
// because the budget ran out is worse than one that fails: empty reads as
// "nothing to fix", which is the wrong thing to believe.

const (
	// A tenth of each bucket is left for whoever else holds this token.
	// On core that is 500 requests an hour, which no sweep comes near; on
	// search it is 3 of 30, which is the one that will ever actually bind.
	reserveDivisor = 10

	// The longest Argus will sit waiting on a secondary limit before
	// giving up and reporting it. Longer than GitHub's usual ask, short
	// enough that a sweep cannot disappear for the rest of the interval.
	maxSecondaryWait = 90 * time.Second

	// What to wait when GitHub names a secondary limit but does not say
	// for how long. Their own guidance is at least a minute.
	defaultSecondaryWait = 60 * time.Second
)

// RateLimitError means a request was refused, or not made at all, because
// of a rate limit. Callers use IsRateLimited to tell it from an ordinary
// failure, because the two deserve different reporting: one is "this is
// broken", the other is "this is incomplete, and here is when to try
// again".
type RateLimitError struct {
	Resource  string
	Reset     time.Time
	Secondary bool
	// Withheld is true when Argus declined to make the request itself
	// rather than GitHub refusing it.
	Withheld bool
	msg      string
}

func (e *RateLimitError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "GitHub's " + e.Resource + " rate limit was reached"
}

// IsRateLimited reports whether an error is a rate limit rather than a
// fault. A rule that fans out should let one of these fail the whole rule
// instead of returning the part of the answer that happened to fit.
func IsRateLimited(err error) bool {
	if err == nil {
		return false
	}
	var r *RateLimitError
	return errors.As(err, &r)
}

// reserveCheck refuses a request that would eat into the reserve.
//
// It reads the last response's headers rather than asking GitHub, so it
// costs nothing. An observation whose window has already reset is stale
// by definition and is ignored - the bucket has refilled since.
func (m *meter) reserveCheck(bucket string) error {
	m.limitMu.Lock()
	limit, ok := m.limits[bucket]
	m.limitMu.Unlock()
	if !ok || limit.Limit <= 0 {
		return nil
	}
	if !limit.Reset.IsZero() && !limit.Reset.After(time.Now()) {
		return nil
	}
	reserve := limit.Limit / reserveDivisor
	if reserve < 1 {
		reserve = 1
	}
	if limit.Remaining > reserve {
		return nil
	}
	return &RateLimitError{
		Resource: bucket,
		Reset:    limit.Reset,
		Withheld: true,
		msg: fmt.Sprintf(
			"stopped short of GitHub's %s rate limit: %d of %d left, and Argus keeps %d back so the "+
				"token stays usable for whatever else you run with it. The limit resets at %s.",
			bucket, limit.Remaining, limit.Limit, reserve, resetWord(limit.Reset)),
	}
}

// secondaryWait reads a refusal and says how long to wait, if waiting is
// the right answer at all.
//
// A 403 or 429 is one of three different things and they need different
// handling: a secondary limit (wait, then retry), a primary limit that is
// exhausted (do not retry - the window may be most of an hour away), or
// an ordinary permission problem (not a limit at all).
func secondaryWait(status int, h http.Header, body string) (time.Duration, bool) {
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return 0, false
	}
	if v := h.Get("retry-after"); v != "" {
		if secs, ok := atoi(v); ok {
			return time.Duration(secs) * time.Second, true
		}
	}
	if strings.Contains(strings.ToLower(body), "secondary rate limit") {
		return defaultSecondaryWait, true
	}
	return 0, false
}

// primaryExhausted reports a bucket drained to nothing. GitHub signals it
// with remaining: 0 on a 403 or 429.
func primaryExhausted(status int, h http.Header, bucket string) *RateLimitError {
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return nil
	}
	remaining, ok := atoi(h.Get("x-ratelimit-remaining"))
	if !ok || remaining > 0 {
		return nil
	}
	resource := h.Get("x-ratelimit-resource")
	if resource == "" {
		resource = bucket
	}
	var reset time.Time
	if sec, ok := atoi(h.Get("x-ratelimit-reset")); ok {
		reset = time.Unix(int64(sec), 0).UTC()
	}
	return &RateLimitError{
		Resource: resource,
		Reset:    reset,
		msg: fmt.Sprintf("GitHub's %s rate limit is exhausted; it resets at %s. "+
			"This result is incomplete rather than empty.", resource, resetWord(reset)),
	}
}

func resetWord(t time.Time) string {
	if t.IsZero() {
		return "an unknown time"
	}
	return t.Local().Format(time.TimeOnly)
}

// hold makes the whole client wait, not just the goroutine that was
// refused. GitHub's guidance on secondary limits is to stop making
// requests, and nine other goroutines carrying on is not stopping.
func (m *meter) hold(d time.Duration) {
	if d <= 0 {
		return
	}
	until := time.Now().Add(d)
	m.limitMu.Lock()
	if until.After(m.pausedUntil) {
		m.pausedUntil = until
	}
	m.limitMu.Unlock()
}

// waitTurn blocks while a hold is in force. One mutex read per request,
// which is the whole cost when nothing is held - the common case.
func (m *meter) waitTurn() {
	for {
		m.limitMu.Lock()
		until := m.pausedUntil
		m.limitMu.Unlock()
		d := time.Until(until)
		if d <= 0 {
			return
		}
		if d > maxSecondaryWait {
			d = maxSecondaryWait
		}
		time.Sleep(d)
	}
}
