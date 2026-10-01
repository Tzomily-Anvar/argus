package rules

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Who is reviewing.
//
// Review is the work no sprint counts: it appears on nobody's board, it
// is in nobody's velocity, and the people who do most of it are usually
// the ones who notice least that they do. A leaderboard makes it
// visible, and a little competitive, which is the fun of it. Two
// periods are kept - this fortnight and the one before - so a rank can
// carry an arrow.
//
// Everything comes from GitHub on each sweep. One search over the pull
// requests updated since the earlier period began, fifty at a time with
// their reviews attached, so the whole board costs a handful of GraphQL
// calls rather than one per pull request. A review of your own pull
// request is not a review and is not counted; neither is a bot's, by
// default.

func init() {
	Register(Rule{
		ID:          "review_leaderboard",
		Title:       "Who is reviewing",
		Description: "Code reviews per person over the current period and the one before, from every pull request updated in that time.",
		Why:         "Review is the work no sprint counts. Seeing who carries it is the first step to sharing it, and a little competition does no harm.",
		Enabled:     true,
		Params: []Param{
			{Name: "days", Desc: "The length of one period, in days. Two periods are kept, so the sweep reads twice this far back.", Default: 14},
			{Name: "exclude_bots", Desc: "Leave out reviews by apps and bots.", Default: true},
		},
		Run: runReviewLeaderboard,
	})
}

// Leaderboard is the rule's answer: two periods of the same length, the
// current one ending now.
type Leaderboard struct {
	Days     int    `json:"days"`
	Current  Period `json:"current"`
	Previous Period `json:"previous"`
	// Truncated says the search hit GitHub's thousand-result ceiling, so
	// the oldest pull requests in the window were not read. The figures
	// are then a floor, and the page says so.
	Truncated bool `json:"truncated"`
}

// Period is one span of time and who reviewed in it, ranked.
type Period struct {
	From    time.Time  `json:"from"`
	To      time.Time  `json:"to"`
	PRs     int        `json:"prs"`     // pull requests that received a review
	Reviews int        `json:"reviews"` // reviews submitted
	Rows    []Reviewer `json:"rows"`
}

// Reviewer is one person's fortnight.
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

// reviewSearchQuery pages through the pull requests updated since a
// date, with every review on each. A review updates the pull request,
// so one updated since the window opened is the superset of what was
// reviewed in it.
const reviewSearchQuery = `
query($q:String!,$after:String){
  search(query:$q,type:ISSUE,first:50,after:$after){
    issueCount
    pageInfo{hasNextPage endCursor}
    nodes{ ... on PullRequest{
      number
      repository{name}
      author{login __typename}
      reviews(first:100){nodes{
        state submittedAt
        author{login __typename}
        comments{totalCount}
      }}
    }}
  }}`

func runReviewLeaderboard(c *Context, v Values) (any, error) {
	days := v.Int("days")
	if days <= 0 {
		days = 14
	}
	span := time.Duration(days) * 24 * time.Hour
	now := c.Now
	current := Period{From: now.Add(-span), To: now}
	previous := Period{From: now.Add(-2 * span), To: current.From}

	prs, truncated, err := c.reviewedPullRequests(previous.From)
	if err != nil {
		return nil, err
	}

	cur := tally(prs, current, v.Bool("exclude_bots"))
	prev := tally(prs, previous, v.Bool("exclude_bots"))
	current.Rows, current.PRs, current.Reviews = cur.rows(), cur.prs, cur.reviews
	previous.Rows, previous.PRs, previous.Reviews = prev.rows(), prev.prs, prev.reviews

	// Last period's standing beside this one, so the page can draw an
	// arrow. Someone absent last time has no previous rank.
	before := map[string]Reviewer{}
	for _, r := range previous.Rows {
		before[r.Login] = r
	}
	for i := range current.Rows {
		if p, ok := before[current.Rows[i].Login]; ok {
			current.Rows[i].PreviousRank, current.Rows[i].PreviousReviews = p.Rank, p.Reviews
		}
	}

	return Leaderboard{Days: days, Current: current, Previous: previous, Truncated: truncated}, nil
}

// reviewedPullRequests reads every pull request in scope updated since
// a date, with its reviews. Pages of fifty; the thousand-result ceiling
// is reported rather than silently hit.
func (c *Context) reviewedPullRequests(since time.Time) ([]map[string]any, bool, error) {
	q := c.Scope.Query() + " is:pr updated:>=" + since.UTC().Format("2006-01-02")
	var out []map[string]any
	var after any
	for {
		data, err := c.Client.GraphQL(reviewSearchQuery, map[string]any{"q": q, "after": after})
		var partial *gh.PartialError
		if err != nil && !errors.As(err, &partial) {
			return nil, false, fmt.Errorf("review leaderboard: %w", err)
		}
		search := gh.Map(data["search"])
		for _, n := range gh.List(search["nodes"]) {
			if pr := gh.Map(n); pr != nil && pr["number"] != nil {
				out = append(out, pr)
			}
		}
		page := gh.Map(search["pageInfo"])
		if !gh.Bool(page["hasNextPage"]) || len(out) >= gh.SearchCeiling {
			return out, int(gh.Num(search["issueCount"])) > len(out), nil
		}
		after = gh.Str(page["endCursor"])
	}
}

// counted are the states that mean a review happened. PENDING is a
// draft nobody has seen, and is not one.
var counted = map[string]bool{"APPROVED": true, "CHANGES_REQUESTED": true, "COMMENTED": true, "DISMISSED": true}

type board struct {
	by      map[string]*Reviewer
	prsOf   map[string]map[string]bool // login -> pull request keys reviewed
	seenPR  map[string]bool
	reviews int
	prs     int
}

// tally counts the reviews submitted inside one period.
func tally(prs []map[string]any, p Period, excludeBots bool) board {
	b := board{by: map[string]*Reviewer{}, prsOf: map[string]map[string]bool{}, seenPR: map[string]bool{}}
	for _, pr := range prs {
		key := gh.Str(gh.Map(pr["repository"])["name"]) + "#" + fmt.Sprint(int(gh.Num(pr["number"])))
		author := gh.Map(pr["author"])
		for _, raw := range gh.List(gh.Map(pr["reviews"])["nodes"]) {
			rv := gh.Map(raw)
			state := strings.ToUpper(gh.Str(rv["state"]))
			if !counted[state] {
				continue
			}
			at, err := time.Parse(time.RFC3339, gh.Str(rv["submittedAt"]))
			if err != nil || at.Before(p.From) || !at.Before(p.To) {
				continue
			}
			who := gh.Map(rv["author"])
			login := gh.Str(who["login"])
			if login == "" || login == gh.Str(author["login"]) {
				continue // nobody, or the author reviewing their own work
			}
			if excludeBots && (gh.Str(who["__typename"]) == "Bot" || strings.HasSuffix(login, "[bot]")) {
				continue
			}
			r := b.by[login]
			if r == nil {
				r = &Reviewer{Login: login}
				b.by[login] = r
				b.prsOf[login] = map[string]bool{}
			}
			r.Reviews++
			r.Comments += int(gh.Num(gh.Map(rv["comments"])["totalCount"]))
			switch state {
			case "APPROVED":
				r.Approvals++
			case "CHANGES_REQUESTED":
				r.ChangesRequested++
			}
			b.prsOf[login][key] = true
			b.reviews++
			if !b.seenPR[key] {
				b.seenPR[key] = true
				b.prs++
			}
		}
	}
	return b
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
