package rules

import (
	"sort"
	"strings"
	"time"
)

// The tally half of the review leaderboard: counting a ReviewLog into
// ranked periods. It is separate from the rule because it runs on
// demand - the server calls it with whatever period a reader picked -
// rather than once per sweep.

// Span is one stretch of time, From inclusive, To exclusive.
type Span struct {
	From time.Time
	To   time.Time
}

// Leaderboard is what the page consumes: two periods, the current one
// and the one it is compared with.
type Leaderboard struct {
	// Days is the rolling period length the page opens on.
	Days int `json:"days"`
	// LookbackDays and Since say how far back the reviews go, so the page
	// can explain a partial period.
	LookbackDays int       `json:"lookback_days"`
	Since        time.Time `json:"since"`
	Current      Period    `json:"current"`
	Previous     Period    `json:"previous"`
	// Truncated says the sweep hit GitHub's thousand-result ceiling, so
	// the figures are floors.
	Truncated bool `json:"truncated"`
}

// Period is one span of time and who reviewed in it, ranked.
type Period struct {
	From    time.Time  `json:"from"`
	To      time.Time  `json:"to"`
	PRs     int        `json:"prs"`     // pull requests that received a review
	Reviews int        `json:"reviews"` // reviews submitted
	Rows    []Reviewer `json:"rows"`
	// Partial says the period starts before the sweep's lookback, so its
	// oldest reviews were never read.
	Partial bool `json:"partial"`
}

// Reviewer is one person's period.
type Reviewer struct {
	Login            string `json:"login"`
	Rank             int    `json:"rank"`
	Reviews          int    `json:"reviews"`
	PRs              int    `json:"prs"`
	Approvals        int    `json:"approvals"`
	ChangesRequested int    `json:"changes_requested"`
	Comments         int    `json:"comments"`
	// PreviousRank is where they stood the period before, 0 if nowhere.
	PreviousRank    int `json:"previous_rank"`
	PreviousReviews int `json:"previous_reviews"`
}

// BuildLeaderboard tallies a log into the two periods asked for, with
// each current row carrying its standing in the previous one so the
// page can draw an arrow. Someone absent last time has no previous
// rank.
func BuildLeaderboard(log ReviewLog, current, previous Span) Leaderboard {
	cur := Tally(log.Reviews, current.From, current.To)
	prev := Tally(log.Reviews, previous.From, previous.To)
	cur.Partial = current.From.Before(log.Since)
	prev.Partial = previous.From.Before(log.Since)

	before := map[string]Reviewer{}
	for _, r := range prev.Rows {
		before[r.Login] = r
	}
	for i := range cur.Rows {
		if p, ok := before[cur.Rows[i].Login]; ok {
			cur.Rows[i].PreviousRank, cur.Rows[i].PreviousReviews = p.Rank, p.Reviews
		}
	}
	return Leaderboard{
		Days: log.Days, LookbackDays: log.LookbackDays, Since: log.Since,
		Current: cur, Previous: prev, Truncated: log.Truncated,
	}
}

// Tally counts the reviews submitted inside one period and ranks them.
func Tally(reviews []Review, from, to time.Time) Period {
	b := board{by: map[string]*Reviewer{}, prsOf: map[string]map[string]bool{}, seenPR: map[string]bool{}}
	for _, rv := range reviews {
		if rv.At.Before(from) || !rv.At.Before(to) {
			continue
		}
		b.add(rv)
	}
	return Period{From: from, To: to, PRs: b.prs, Reviews: b.reviews, Rows: b.rows()}
}

type board struct {
	by      map[string]*Reviewer
	prsOf   map[string]map[string]bool // login -> pull request keys reviewed
	seenPR  map[string]bool
	reviews int
	prs     int
}

func (b *board) add(rv Review) {
	r := b.by[rv.Login]
	if r == nil {
		r = &Reviewer{Login: rv.Login}
		b.by[rv.Login] = r
		b.prsOf[rv.Login] = map[string]bool{}
	}
	r.Reviews++
	r.Comments += rv.Comments
	switch rv.State {
	case "APPROVED":
		r.Approvals++
	case "CHANGES_REQUESTED":
		r.ChangesRequested++
	}
	b.prsOf[rv.Login][rv.PR] = true
	b.reviews++
	if !b.seenPR[rv.PR] {
		b.seenPR[rv.PR] = true
		b.prs++
	}
}

// rows ranks the board: most reviews first, then most pull requests,
// then most comments, then by name so the order is stable. Equal
// figures share a rank, as they do on any podium.
func (b board) rows() []Reviewer {
	out := make([]Reviewer, 0, len(b.by))
	for login, r := range b.by {
		r.PRs = len(b.prsOf[login])
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, z := out[i], out[j]
		if a.Reviews != z.Reviews {
			return a.Reviews > z.Reviews
		}
		if a.PRs != z.PRs {
			return a.PRs > z.PRs
		}
		if a.Comments != z.Comments {
			return a.Comments > z.Comments
		}
		return strings.ToLower(a.Login) < strings.ToLower(z.Login)
	})
	for i := range out {
		if i > 0 && out[i].Reviews == out[i-1].Reviews && out[i].PRs == out[i-1].PRs && out[i].Comments == out[i-1].Comments {
			out[i].Rank = out[i-1].Rank
		} else {
			out[i].Rank = i + 1
		}
	}
	return out
}
