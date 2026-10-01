package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/rules"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
)

// The review leaderboard, tallied on demand.
//
// The sweep's review_leaderboard rule stores every review it read, not a
// tally, so the board can be counted over any period a reader picks
// without going back to GitHub: the rolling fortnight the page opens
// on, or a Jira sprint and the one before it. This is the counting.

// leaderboardAvailable says whether the Leaderboard tool is live: the
// pull request sweep is on and so is the rule that feeds it. There is
// no ARGUS_TOOLS entry; turning the rule off is how the tool is hidden.
func leaderboardAvailable() bool {
	rule, ok := rules.Get("review_leaderboard")
	return ok && config.ToolEnabled("pr") && rules.IsEnabled(rule)
}

// leaderboardResponse answers GET /api/leaderboard from a snapshot.
//
// With no query the current period is the rule's `days` ending at the
// sweep, and the previous period the same length before it. With
// from/to it is that span, and prev_from/prev_to the comparison; left
// out, the comparison is the same length again before `from`. A
// period that starts before the sweep's lookback is marked partial
// rather than silently short.
func leaderboardResponse(snap sweep.Snapshot, q url.Values) (int, any) {
	res, ok := snap.Results["review_leaderboard"]
	if !ok {
		if !snap.Ready {
			return http.StatusServiceUnavailable, map[string]string{"error": "the first sweep has not finished yet"}
		}
		return http.StatusNotFound, map[string]string{"error": "the review_leaderboard rule is off"}
	}
	if res.Error != "" {
		return http.StatusBadGateway, map[string]string{"error": res.Error}
	}
	log, ok := res.Data.(rules.ReviewLog)
	if !ok {
		return http.StatusInternalServerError, map[string]string{"error": "the review_leaderboard result has an unexpected shape"}
	}
	sweptAt, err := time.Parse(time.RFC3339, snap.SweptAt)
	if err != nil {
		return http.StatusServiceUnavailable, map[string]string{"error": "the first sweep has not finished yet"}
	}

	current, previous, err := leaderboardSpans(q, sweptAt, log.Days)
	if err != nil {
		return http.StatusBadRequest, map[string]string{"error": err.Error()}
	}
	return http.StatusOK, rules.BuildLeaderboard(log, current, previous)
}

// leaderboardSpans reads the two periods out of the query, or supplies
// the rolling defaults. The pairs come whole or not at all: a lone
// `from` is a mistake, not a half-open range.
func leaderboardSpans(q url.Values, sweptAt time.Time, days int) (current, previous rules.Span, err error) {
	if days <= 0 {
		days = 14
	}
	current, set, err := spanOf(q, "from", "to")
	if err != nil {
		return current, previous, err
	}
	if !set {
		current = rules.Span{From: sweptAt.Add(-time.Duration(days) * 24 * time.Hour), To: sweptAt}
	}
	previous, set, err = spanOf(q, "prev_from", "prev_to")
	if err != nil {
		return current, previous, err
	}
	if !set {
		previous = rules.Span{From: current.From.Add(-current.To.Sub(current.From)), To: current.From}
	}
	return current, previous, nil
}

func spanOf(q url.Values, fromKey, toKey string) (rules.Span, bool, error) {
	fromRaw, toRaw := q.Get(fromKey), q.Get(toKey)
	if fromRaw == "" && toRaw == "" {
		return rules.Span{}, false, nil
	}
	if fromRaw == "" || toRaw == "" {
		return rules.Span{}, false, fmt.Errorf("%s and %s come together", fromKey, toKey)
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return rules.Span{}, false, fmt.Errorf("%s is not an RFC 3339 time", fromKey)
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return rules.Span{}, false, fmt.Errorf("%s is not an RFC 3339 time", toKey)
	}
	if !from.Before(to) {
		return rules.Span{}, false, errors.New(fromKey + " must be before " + toKey)
	}
	return rules.Span{From: from, To: to}, true, nil
}
