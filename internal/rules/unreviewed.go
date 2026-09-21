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

	candidates, err := c.Search(q)
	if err != nil {
		return nil, err
	}

	// GitHub's search index lags reality by a few minutes, so review:none
	// alone is not trustworthy. Confirm against the PR itself, in parallel.
	checked := gh.PMap(candidates, c.Concurrency, func(it map[string]any) map[string]any {
		pr, err := c.Client.Get("/repos/"+c.Org+"/"+RepoName(it)+"/pulls/"+NumberStr(it), nil)
		if err != nil {
			return nil
		}
		if len(gh.List(pr["requested_reviewers"])) > 0 || len(gh.List(pr["requested_teams"])) > 0 {
			return nil
		}
		return Row(it, c.Now, map[string]any{"labels": strs(Labels(it))})
	})

	return SortByAge(Compact(checked)), nil
}
