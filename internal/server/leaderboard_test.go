package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/rules"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
)

var sweptAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// A snapshot whose review log holds one review a day for the last
// thirty, alternating between two reviewers, so any period's figures
// can be worked out by hand.
func leaderboardSnapshot() sweep.Snapshot {
	since := sweptAt.AddDate(0, 0, -60)
	var reviews []rules.Review
	for d := 1; d <= 30; d++ {
		login := "ann"
		if d%2 == 0 {
			login = "ben"
		}
		reviews = append(reviews, rules.Review{Login: login, PR: "api#" + string(rune('0'+d%10)), Author: "cal", At: sweptAt.AddDate(0, 0, -d), State: "APPROVED"})
	}
	log := rules.ReviewLog{LookbackDays: 60, Since: since, Days: 14, Reviews: reviews}
	return sweep.Snapshot{
		Ready:   true,
		SweptAt: sweptAt.Format(time.RFC3339),
		Results: map[string]sweep.Result{"review_leaderboard": {RuleID: "review_leaderboard", Data: log}},
	}
}

// No query means the rule's rolling period ending at the sweep, and
// the same length again before it.
func TestLeaderboardDefaultsToTheRollingPeriod(t *testing.T) {
	status, body := leaderboardResponse(leaderboardSnapshot(), url.Values{})
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, body)
	}
	lb := body.(rules.Leaderboard)
	if !lb.Current.To.Equal(sweptAt) || !lb.Current.From.Equal(sweptAt.AddDate(0, 0, -14)) {
		t.Errorf("current = %s to %s", lb.Current.From, lb.Current.To)
	}
	if !lb.Previous.To.Equal(lb.Current.From) || !lb.Previous.From.Equal(sweptAt.AddDate(0, 0, -28)) {
		t.Errorf("previous = %s to %s", lb.Previous.From, lb.Previous.To)
	}
	// A period starts inclusive and ends exclusive, so day 14 opens the
	// current one and day 28 opens the previous: fourteen reviews each.
	if lb.Current.Reviews != 14 || lb.Previous.Reviews != 14 {
		t.Errorf("reviews = %d and %d, want 14 and 14", lb.Current.Reviews, lb.Previous.Reviews)
	}
	if lb.Current.Partial || lb.Previous.Partial {
		t.Error("the defaults sit inside the lookback")
	}
}

// A sprint's dates tally that sprint, and the one before it supplies
// the arrows. A range reaching back past the lookback is partial.
func TestLeaderboardTalliesTheRangeAsked(t *testing.T) {
	q := url.Values{}
	q.Set("from", sweptAt.AddDate(0, 0, -9).Format(time.RFC3339))
	q.Set("to", sweptAt.AddDate(0, 0, -2).Format(time.RFC3339))
	q.Set("prev_from", sweptAt.AddDate(0, 0, -70).Format(time.RFC3339))
	q.Set("prev_to", sweptAt.AddDate(0, 0, -9).Format(time.RFC3339))
	status, body := leaderboardResponse(leaderboardSnapshot(), q)
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, body)
	}
	lb := body.(rules.Leaderboard)
	// Days 3..9 inclusive: seven reviews, four of them ann's.
	// (Day 2 is the exclusive end; day 9 is the inclusive start.)
	if lb.Current.Reviews != 7 || len(lb.Current.Rows) != 2 || lb.Current.Rows[0].Login != "ann" || lb.Current.Rows[0].Reviews != 4 {
		t.Errorf("current = %+v", lb.Current)
	}
	if lb.Previous.Reviews != 21 || !lb.Previous.Partial || lb.Current.Partial {
		t.Errorf("previous reviews %d partial %v, current partial %v", lb.Previous.Reviews, lb.Previous.Partial, lb.Current.Partial)
	}
	// Days 10..30 before that: ben had 11 of the 21 to ann's 10, so he
	// led then.
	if ann := lb.Current.Rows[0]; ann.PreviousRank != 2 || ann.PreviousReviews != 10 {
		t.Errorf("ann's previous standing = rank %d with %d reviews, want 2 and 10", ann.PreviousRank, ann.PreviousReviews)
	}

	// Without a comparison pair, the previous period is the same length
	// again before `from`.
	q.Del("prev_from")
	q.Del("prev_to")
	_, body = leaderboardResponse(leaderboardSnapshot(), q)
	if lb := body.(rules.Leaderboard); !lb.Previous.To.Equal(lb.Current.From) || !lb.Previous.From.Equal(sweptAt.AddDate(0, 0, -16)) {
		t.Errorf("defaulted previous = %s to %s", lb.Previous.From, lb.Previous.To)
	}
}

func TestLeaderboardRejectsBadRanges(t *testing.T) {
	at := func(d int) string { return sweptAt.AddDate(0, 0, d).Format(time.RFC3339) }
	cases := map[string]url.Values{
		"from after to":     {"from": {at(-1)}, "to": {at(-5)}},
		"from equal to":     {"from": {at(-1)}, "to": {at(-1)}},
		"a lone from":       {"from": {at(-1)}},
		"not a time":        {"from": {"yesterday"}, "to": {at(0)}},
		"previous reversed": {"from": {at(-5)}, "to": {at(0)}, "prev_from": {at(-5)}, "prev_to": {at(-10)}},
		"a lone prev_to":    {"from": {at(-5)}, "to": {at(0)}, "prev_to": {at(-5)}},
	}
	for name, q := range cases {
		if status, _ := leaderboardResponse(leaderboardSnapshot(), q); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, status)
		}
	}
}

// Before the first sweep there is nothing to count; with the rule off
// there never will be; a rule that failed passes its error on.
func TestLeaderboardSaysWhyItCannotAnswer(t *testing.T) {
	if status, _ := leaderboardResponse(sweep.Snapshot{}, url.Values{}); status != http.StatusServiceUnavailable {
		t.Errorf("before the first sweep: %d, want 503", status)
	}
	ready := sweep.Snapshot{Ready: true, SweptAt: sweptAt.Format(time.RFC3339), Results: map[string]sweep.Result{}}
	if status, _ := leaderboardResponse(ready, url.Values{}); status != http.StatusNotFound {
		t.Errorf("rule off: %d, want 404", status)
	}
	ready.Results["review_leaderboard"] = sweep.Result{RuleID: "review_leaderboard", Error: "rate limited"}
	if status, body := leaderboardResponse(ready, url.Values{}); status != http.StatusBadGateway || body.(map[string]string)["error"] != "rate limited" {
		t.Errorf("rule failed: %d %v", status, body)
	}
}

// The tool is listed whenever the pull request tool and the rule are
// on; the rule's switch is what hides it.
func TestLeaderboardToolFollowsTheRule(t *testing.T) {
	available := func() bool {
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tools", nil))
		var body struct {
			Tools []struct {
				ID        string `json:"id"`
				Available bool   `json:"available"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		for _, tool := range body.Tools {
			if tool.ID == "leaderboard" {
				return tool.Available
			}
		}
		t.Fatal("no leaderboard entry in /api/tools")
		return false
	}
	t.Setenv("ARGUS_TOOLS", "pr")
	t.Setenv("ARGUS_RULE_REVIEW_LEADERBOARD_ENABLED", "")
	if !available() {
		t.Error("with pr on and the rule at its default the tool should be live")
	}
	t.Setenv("ARGUS_RULE_REVIEW_LEADERBOARD_ENABLED", "false")
	if available() {
		t.Error("turning the rule off should hide the tool")
	}
	t.Setenv("ARGUS_RULE_REVIEW_LEADERBOARD_ENABLED", "")
	t.Setenv("ARGUS_TOOLS", "sprint")
	if available() {
		t.Error("without the pull request sweep there is nothing to count")
	}
}
