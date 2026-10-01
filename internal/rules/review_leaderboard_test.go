package rules

import (
	"testing"
	"time"
)

// The tally judges one period from the reviews on each pull request:
// inside the window, by somebody other than the author, not pending,
// and not a bot unless asked.
func TestReviewTallyCountsWhatCounts(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cur := Period{From: now.AddDate(0, 0, -14), To: now}
	stamp := func(daysAgo int) string { return now.AddDate(0, 0, -daysAgo).Format(time.RFC3339) }
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
	prs := []map[string]any{
		pr("api", 1, "alice",
			review("bob", "User", "APPROVED", 2, 3),
			review("carol", "User", "CHANGES_REQUESTED", 3, 5),
			review("carol", "User", "APPROVED", 1, 0),
			review("alice", "User", "COMMENTED", 1, 2),    // her own PR
			review("dependabot", "Bot", "APPROVED", 1, 0), // a bot
			review("bob", "User", "PENDING", 1, 0),        // not submitted
			review("dave", "User", "APPROVED", 20, 0),     // last period
		),
		pr("web", 2, "bob",
			review("carol", "User", "COMMENTED", 5, 1),
		),
	}

	b := tally(prs, cur, true)
	rows := b.rows()
	if b.reviews != 4 || b.prs != 2 {
		t.Errorf("reviews %d prs %d, want 4 and 2", b.reviews, b.prs)
	}
	if len(rows) != 2 || rows[0].Login != "carol" || rows[0].Rank != 1 || rows[1].Login != "bob" || rows[1].Rank != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	c := rows[0]
	if c.Reviews != 3 || c.PRs != 2 || c.Approvals != 1 || c.ChangesRequested != 1 || c.Comments != 6 {
		t.Errorf("carol = %+v", c)
	}

	// Last period holds dave alone; bots count when asked.
	prev := Period{From: now.AddDate(0, 0, -28), To: cur.From}
	if rows := tally(prs, prev, true).rows(); len(rows) != 1 || rows[0].Login != "dave" {
		t.Errorf("previous = %+v", rows)
	}
	if rows := tally(prs, cur, false).rows(); len(rows) != 3 {
		t.Errorf("with bots = %+v", rows)
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
