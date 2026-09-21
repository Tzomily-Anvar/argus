package rules

import "github.com/Tzomily-Anvar/argus/internal/gh"

func init() {
	Register(Rule{
		ID:          "merge_readiness",
		Title:       "Open pull requests",
		Description: "Every open, review-ready pull request, with its review decision and check status attached.",
		Why:         "The full inventory. The status column is the point: it separates what is approved and green from what is still waiting on a review or a fix.",
		Enabled:     true,
		Params: append([]Param{
			{Name: "exclude_drafts", Desc: "Skip draft pull requests.", Default: true},
			{Name: "exclude_authors", Desc: "Authors to ignore entirely, comma separated.", Default: []string{"app/dependabot"}},
		}, checkParams()...),
		Run: runMergeReadiness,
	})
}

func runMergeReadiness(c *Context, v Values) (any, error) {
	q := "is:pr is:open"
	if v.Bool("exclude_drafts") {
		q += " draft:false"
	}
	for _, a := range v.Strs("exclude_authors") {
		q += " -author:" + a
	}

	items, err := c.Search(q)
	if err != nil {
		return nil, err
	}

	qa, ignore := v.Str("qa_label"), v.Strs("ignore_checks")
	rows := gh.PMap(items, c.Concurrency, func(it map[string]any) map[string]any {
		checks := prChecks(c, RepoName(it), Number(it), qa, ignore)
		if checks == nil {
			// Dropping the row made the rule render empty whenever checks
			// could not be read - which looks like "nothing is open"
			// rather than "check status is unavailable". Showing the pull
			// request with unknown status is the honest answer.
			checks = noChecks()
		}
		return Row(it, c.Now, checks)
	})

	clean := Compact(rows)
	return map[string]any{
		"rows": Rows(clean),
		// Suggestions only - nothing above has been reclassified.
		"policy_hints": detectPolicyHints(clean, "merge_readiness", ignore),
	}, nil
}
