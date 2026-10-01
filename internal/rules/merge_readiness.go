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
			{Name: "exclude_drafts", Desc: "Keep draft pull requests out of the open list and show them under their own tab instead.", Default: true},
			{Name: "exclude_authors", Desc: "Authors to ignore entirely, comma separated.", Default: []string{"app/dependabot"}},
		}, checkParams()...),
		Run: runMergeReadiness,
	})
}

func runMergeReadiness(c *Context, v Values) (any, error) {
	// Drafts are read like everything else and separated afterwards:
	// kept apart from the open list by default, so that list stays about
	// what is asking for review, but shown under their own heading rather
	// than dropped. A repository whose only open pull requests are drafts
	// used to look like one with nothing open, and the first question
	// anyone asked was where they went.
	q := "is:pr is:open"
	apart, authors := v.Bool("exclude_drafts"), v.Strs("exclude_authors")
	for _, a := range authors {
		q += " -author:" + a
	}
	items, err := c.OpenPRsWhere(q, func(it map[string]any) bool {
		for _, a := range authors {
			if MatchesAuthor(it, a) {
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	qa, ignore := v.Str("qa_label"), v.Strs("ignore_checks")
	got := gh.PMap(items, c.Concurrency, func(it map[string]any) checked {
		checks, err := prChecks(c, RepoName(it), Number(it), qa, ignore)
		if checks == nil {
			// Dropping the row made the rule render empty whenever checks
			// could not be read - which looks like "nothing is open"
			// rather than "check status is unavailable". Showing the pull
			// request with unknown status is the honest answer.
			checks = noChecks()
		}
		checks["draft"] = gh.Bool(it["draft"])
		return checked{row: Row(it, c.Now, checks), err: err}
	})

	rows, err := unpack(got)
	if err != nil {
		return nil, err
	}
	clean := Compact(rows)
	open, drafts := clean, []map[string]any{}
	if apart {
		open, drafts = []map[string]any{}, []map[string]any{}
		for _, r := range clean {
			if gh.Bool(r["draft"]) {
				drafts = append(drafts, r)
			} else {
				open = append(open, r)
			}
		}
	}
	return map[string]any{
		"rows":   Rows(open),
		"drafts": Rows(drafts),
		// Suggestions only - nothing above has been reclassified.
		"policy_hints": detectPolicyHints(open, "merge_readiness", ignore),
	}, nil
}
