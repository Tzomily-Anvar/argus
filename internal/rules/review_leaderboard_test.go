package rules

import (
	"testing"
	"time"
)

var leaderboardNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// Two pull requests with the usual mix of reviews on them, as the
// GraphQL search hands them back.
func samplePullRequests() []map[string]any {
	stamp := func(daysAgo int) string { return leaderboardNow.AddDate(0, 0, -daysAgo).Format(time.RFC3339) }
	review := func(login, typ, state string, daysAgo, comments int) map[string]any {
		return map[string]any{
			"state": state, "submittedAt": stamp(daysAgo),
			"author":   map[string]any{"login": login, "__typename": typ},
			"comments": map[string]any{"totalCount": float64(comments)},
		}
	}
	pr := func(repo string, n int, author string, reviews ...map[string]any) map[string]any {
		nodes := make([]any, 0, len(reviews))
		for _, r := range reviews {
			nodes = append(nodes, r)
		}
		return map[string]any{
			"number": float64(n), "repository": map[string]any{"name": repo},
			"author":  map[string]any{"login": author, "__typename": "User"},
			"reviews": map[string]any{"nodes": nodes},
		}
	}
	return []map[string]any{
		pr("api", 1, "alice",
			review("bob", "User", "APPROVED", 2, 3),
			review("carol", "User", "CHANGES_REQUESTED", 3, 5),
			review("carol", "User", "APPROVED", 1, 0),
			review("alice", "User", "COMMENTED", 1, 2),    // her own PR
			review("dependabot", "Bot", "APPROVED", 1, 0), // a bot
			review("bob", "User", "PENDING", 1, 0),        // not submitted
			review("dave", "User", "APPROVED", 20, 0),     // last period
			review("erin", "User", "APPROVED", 90, 0),     // before the lookback
		),
		pr("web", 2, "bob",
			review("carol", "User", "COMMENTED", 5, 1),
		),
	}
}

// The log keeps the reviews that count: by somebody other than the
// author, not pending, inside the lookback, and not a bot unless asked.
func TestReviewLogKeepsWhatCounts(t *testing.T) {
	since := leaderboardNow.AddDate(0, 0, -60)
	reviews := collectReviews(samplePullRequests(), since, true)
	if len(reviews) != 5 {
		t.Fatalf("kept %d reviews, want 5: %+v", len(reviews), reviews)
	}
	for _, rv := range reviews {
		switch {
		case rv.Login == "alice", rv.Login == "dependabot", rv.Login == "erin", rv.State == "PENDING":
			t.Errorf("kept a review that does not count: %+v", rv)
		case rv.PR != "api#1" && rv.PR != "web#2":
			t.Errorf("odd pull request key %q", rv.PR)
		}
	}
	if with := collectReviews(samplePullRequests(), since, false); len(with) != 6 {
		t.Errorf("with bots kept %d, want 6", len(with))
	}
}

// A tally judges one period from the log, and the two-period build
// carries last period's rank onto this one.
func TestLeaderboardTalliesOnDemand(t *testing.T) {
	since := leaderboardNow.AddDate(0, 0, -60)
	log := ReviewLog{LookbackDays: 60, Since: since, Days: 14, Reviews: collectReviews(samplePullRequests(), since, true)}
	cur := Span{From: leaderboardNow.AddDate(0, 0, -14), To: leaderboardNow}
	prev := Span{From: cur.From.AddDate(0, 0, -14), To: cur.From}

	lb := BuildLeaderboard(log, cur, prev)
	p := lb.Current
	if p.Reviews != 4 || p.PRs != 2 {
		t.Errorf("reviews %d prs %d, want 4 and 2", p.Reviews, p.PRs)
	}
	if len(p.Rows) != 2 || p.Rows[0].Login != "carol" || p.Rows[0].Rank != 1 || p.Rows[1].Login != "bob" || p.Rows[1].Rank != 2 {
		t.Fatalf("rows = %+v", p.Rows)
	}
	if c := p.Rows[0]; c.Reviews != 3 || c.PRs != 2 || c.Approvals != 1 || c.ChangesRequested != 1 || c.Comments != 6 {
		t.Errorf("carol = %+v", c)
	}
	if c := p.Rows[0]; c.PreviousRank != 0 {
		t.Errorf("carol was not on last period's board, got previous rank %d", c.PreviousRank)
	}
	if rows := lb.Previous.Rows; len(rows) != 1 || rows[0].Login != "dave" {
		t.Errorf("previous = %+v", rows)
	}
	if lb.Current.Partial || lb.Previous.Partial {
		t.Error("both periods sit inside the lookback and should not be partial")
	}

	// Somebody on both boards carries their old rank; a period reaching
	// back past the lookback is marked partial.
	wide := BuildLeaderboard(log, Span{From: since.AddDate(0, 0, -30), To: leaderboardNow}, prev)
	if !wide.Current.Partial || wide.Previous.Partial {
		t.Errorf("partial flags = %v %v, want true false", wide.Current.Partial, wide.Previous.Partial)
	}
	var dave *Reviewer
	for i := range wide.Current.Rows {
		if wide.Current.Rows[i].Login == "dave" {
			dave = &wide.Current.Rows[i]
		}
	}
	if dave == nil || dave.PreviousRank != 1 || dave.PreviousReviews != 1 {
		t.Errorf("dave = %+v, want previous rank 1 with 1 review", dave)
	}
}

// Equal figures share a rank; the next distinct figure resumes the count.
func TestLeaderboardRanksTiesTogether(t *testing.T) {
	b := board{by: map[string]*Reviewer{
		"a": {Login: "a", Reviews: 5}, "b": {Login: "b", Reviews: 5}, "c": {Login: "c", Reviews: 1},
	}, prsOf: map[string]map[string]bool{"a": {"x": true}, "b": {"x": true}, "c": {"x": true}}}
	rows := b.rows()
	if rows[0].Rank != 1 || rows[1].Rank != 1 || rows[2].Rank != 3 {
		t.Errorf("ranks = %d %d %d, want 1 1 3", rows[0].Rank, rows[1].Rank, rows[2].Rank)
	}
}
