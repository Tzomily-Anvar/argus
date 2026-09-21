package rules

func init() {
	Register(Rule{
		ID:          "review_requested",
		Title:       "Waiting on your review",
		Description: "Other people's open pull requests where a review is requested from you, or from a team you belong to. Not your own work - that is listed separately.",
		Why:         "These are blocked on you specifically. Nothing else on the dashboard is as directly your move.",
		Enabled:     true,
		Params: []Param{
			{Name: "include_teams", Desc: "Also count reviews requested from your teams, not just you personally.", Default: true},
		},
		Run: runReviewRequested,
	})
}

func runReviewRequested(c *Context, v Values) (any, error) {
	queries := []string{"is:pr is:open review-requested:" + c.Me}
	if v.Bool("include_teams") {
		for _, t := range c.Teams {
			queries = append(queries, "is:pr is:open team-review-requested:"+c.Org+"/"+t)
		}
	}

	seen := map[string]bool{}
	var out []map[string]any
	for _, q := range queries {
		items, err := c.Search(q)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			// A PR requested from both you and your team appears in two
			// queries; dedupe on the canonical URL.
			url := gstr(it["html_url"])
			if seen[url] {
				continue
			}
			seen[url] = true
			out = append(out, Row(it, c.Now, nil))
		}
	}
	return SortByAge(out), nil
}
