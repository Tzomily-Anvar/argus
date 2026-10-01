package rules

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Who is reviewing.
//
// Review is the work no sprint counts: it appears on nobody's board, it
// is in nobody's velocity, and the people who do most of it are usually
// the ones who notice least that they do. A leaderboard makes it
// visible, and a little competitive, which is the fun of it. It is a
// thank-you to the people doing the reviewing, never a target: review
// is teamwork, and a number that became a quota would stop measuring
// anything worth knowing.
//
// Everything comes from GitHub on each sweep. One search over the pull
// requests updated inside the lookback, fifty at a time with their
// reviews attached, so the whole board costs a handful of GraphQL calls
// rather than one per pull request. The rule does not tally. It hands
// back every review it read, with a timestamp, and the server counts
// them for whatever period a reader asks for - the rolling fortnight by
// default, or a Jira sprint - so a change of period costs nothing from
// GitHub. The lookback is long enough for two sprints of four weeks,
// which is what the sprint view needs to draw an arrow. A review of
// your own pull request is not a review and is not counted; neither is
// a bot's, by default.

func init() {
	Register(Rule{
		ID:          "review_leaderboard",
		Title:       "Who is reviewing",
		Description: "Every code review submitted inside the lookback, so the Leaderboard tool can count them per person over a rolling period or a sprint.",
		Why:         "Review is the work no sprint counts. Seeing who carries it is the first step to sharing it. For fun: review is teamwork, and this board is a thank-you to the people doing it, not a target for anyone.",
		Enabled:     true,
		Params: []Param{
			{Name: "lookback_days", Desc: "How far back the sweep reads reviews. Long enough for two sprints, so a sprint on the board can be compared with the one before.", Default: 60},
			{Name: "days", Desc: "The length of the rolling period the Leaderboard opens on, in days. A fortnight by default.", Default: 14},
			{Name: "exclude_bots", Desc: "Leave out reviews by apps and bots.", Default: true},
		},
		Run: runReviewLeaderboard,
	})
}

// ReviewLog is the rule's answer: the reviews themselves, not a tally.
type ReviewLog struct {
	// LookbackDays is how far back the sweep read, and Since the moment
	// that is. A period starting earlier than Since is missing its
	// oldest reviews, and whoever tallies it should say so.
	LookbackDays int       `json:"lookback_days"`
	Since        time.Time `json:"since"`
	// Days is the rolling period length the page opens on.
	Days int `json:"days"`
	// Truncated says the search hit GitHub's thousand-result ceiling, so
	// the oldest pull requests in the window were not read. Any tally is
	// then a floor, and the page says so.
	Truncated bool `json:"truncated"`
	// PRCount is how many pull requests were read, reviewed or not.
	PRCount int      `json:"pr_count"`
	Reviews []Review `json:"reviews"`
}

// Review is one submitted review that counts: not the author's own, not
// pending, and not a bot's unless bots were asked for.
type Review struct {
	Login    string    `json:"login"`
	PR       string    `json:"pr"` // repo#number
	Author   string    `json:"author"`
	At       time.Time `json:"at"`
	State    string    `json:"state"`
	Comments int       `json:"comments"`
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
	lookback := v.Int("lookback_days")
	if lookback <= 0 {
		lookback = 60
	}
	days := v.Int("days")
	if days <= 0 {
		days = 14
	}
	since := c.Now.Add(-time.Duration(lookback) * 24 * time.Hour)

	prs, truncated, err := c.reviewedPullRequests(since)
	if err != nil {
		return nil, err
	}
	reviews := collectReviews(prs, since, v.Bool("exclude_bots"))
	return ReviewLog{
		LookbackDays: lookback, Since: since, Days: days,
		Truncated: truncated, PRCount: len(prs), Reviews: reviews,
	}, nil
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

// collectReviews flattens the pull requests into the reviews that
// count, submitted since the lookback began. A pull request updated
// inside the window can carry reviews from long before it, which would
// be missing for every other pull request of their age; dropping them
// keeps the log consistent with its own Since.
func collectReviews(prs []map[string]any, since time.Time, excludeBots bool) []Review {
	out := []Review{}
	for _, pr := range prs {
		key := gh.Str(gh.Map(pr["repository"])["name"]) + "#" + fmt.Sprint(int(gh.Num(pr["number"])))
		author := gh.Str(gh.Map(pr["author"])["login"])
		for _, raw := range gh.List(gh.Map(pr["reviews"])["nodes"]) {
			rv := gh.Map(raw)
			state := strings.ToUpper(gh.Str(rv["state"]))
			if !counted[state] {
				continue
			}
			at, err := time.Parse(time.RFC3339, gh.Str(rv["submittedAt"]))
			if err != nil || at.Before(since) {
				continue
			}
			who := gh.Map(rv["author"])
			login := gh.Str(who["login"])
			if login == "" || login == author {
				continue // nobody, or the author reviewing their own work
			}
			if excludeBots && (gh.Str(who["__typename"]) == "Bot" || strings.HasSuffix(login, "[bot]")) {
				continue
			}
			out = append(out, Review{
				Login: login, PR: key, Author: author, At: at, State: state,
				Comments: int(gh.Num(gh.Map(rv["comments"])["totalCount"])),
			})
		}
	}
	return out
}
