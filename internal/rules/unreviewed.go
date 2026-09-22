package rules

import "github.com/Tzomily-Anvar/argus/internal/gh"

func init() {
	Register(Rule{
		ID:          "unreviewed",
		Title:       "Nobody has picked these up",
		Description: "Open, review-ready pull requests with no reviewer and no team assigned.",
		Why:         "These are not blocked on any particular person, which is exactly why they stall. Someone has to volunteer.",
		Enabled:     true,
		Params: []Param{
			{Name: "exclude_drafts", Desc: "Skip draft pull requests, which are not asking for review yet.", Default: true},
			{Name: "exclude_authors", Desc: "Authors to ignore entirely, comma separated.", Default: []string{"app/dependabot"}},
		},
		Run: runUnreviewed,
	})
}

func runUnreviewed(c *Context, v Values) (any, error) {
	q := "is:pr is:open review:none"
	if v.Bool("exclude_drafts") {
		q += " draft:false"
	}
	for _, a := range v.Strs("exclude_authors") {
		q += " -author:" + a
	}

	// review:none is a search qualifier with no local equivalent, so this
	// is one of the two searches that cannot come off the shared listing.
	candidates, err := c.Search(q)
	if err != nil {
		return nil, err
	}

	// GitHub's search index lags reality by a few minutes, so review:none
	// alone is not trustworthy. Confirm against the pull request itself.
	//
	// That confirmation used to be a REST fetch of the whole pull request
	// for one field. The inventory rule is already fetching these same
	// pull requests over GraphQL, so asking it for the review requests
	// too makes this rule's verification free whenever the two sets
	// overlap - which, both being open non-draft pull requests, is nearly
	// always.
	got := gh.PMap(candidates, c.Concurrency, func(it map[string]any) checked {
		pr, err := c.PullRequest(RepoName(it), Number(it))
		if err != nil || pr == nil {
			return checked{err: err}
		}
		if reviewRequested(pr) {
			return checked{}
		}
		return checked{row: Row(it, c.Now, map[string]any{"labels": strs(Labels(it))})}
	})

	rows, err := unpack(got)
	if err != nil {
		return nil, err
	}
	return SortByAge(Compact(rows)), nil
}
