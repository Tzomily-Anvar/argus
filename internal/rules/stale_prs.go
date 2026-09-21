package rules

func init() {
	Register(Rule{
		ID:          "stale_prs",
		Title:       "Stale pull requests",
		Description: "Open pull requests older than the age threshold.",
		Why:         "Long-lived branches drift from main and get harder to merge the longer they sit. Either finish them or close them.",
		Enabled:     true,
		Params: []Param{
			{Name: "days", Desc: "A pull request older than this many days counts as stale.", Default: 14},
			{Name: "count_bots_separately", Desc: "Report bot-authored stale PRs as a single count rather than listing each one.", Default: true},
		},
		Run: runStalePRs,
	})
}

func runStalePRs(c *Context, v Values) (any, error) {
	cut := c.Now.AddDate(0, 0, -v.Int("days")).Format("2006-01-02")
	items, err := c.Search("is:pr is:open created:<" + cut)
	if err != nil {
		return nil, err
	}

	separateBots := v.Bool("count_bots_separately")
	var human []map[string]any
	bots := 0
	for _, it := range items {
		if separateBots && IsBot(AuthorLogin(it)) {
			// Dependabot reliably has stale PRs open; listing each one
			// buries the human PRs that actually need a decision.
			bots++
			continue
		}
		human = append(human, Row(it, c.Now, nil))
	}

	return map[string]any{
		"rows":      SortByAge(human),
		"bot_count": bots,
	}, nil
}
