package rules

import "github.com/Tzomily-Anvar/argus/internal/gh"

func init() {
	Register(Rule{
		ID:          "my_prs",
		Title:       "Your open pull requests",
		Description: "Every open pull request you authored, drafts included. The opposite of the review queue, which holds other people's work waiting on you.",
		Why:         "Drafts are deliberately included here. A draft you opened and forgot is invisible everywhere else, and that is exactly how work goes stale.",
		Enabled:     true,
		Params:      checkParams(),
		Run:         runMyPRs,
	})
}

func runMyPRs(c *Context, v Values) (any, error) {
	items, err := c.OpenPRsWhere("is:pr is:open author:"+c.Me, func(it map[string]any) bool {
		return MatchesAuthor(it, c.Me)
	})
	if err != nil {
		return nil, err
	}

	qa, ignore := v.Str("qa_label"), v.Strs("ignore_checks")
	got := gh.PMap(items, c.Concurrency, func(it map[string]any) checked {
		checks, err := prChecks(c, RepoName(it), Number(it), qa, ignore)
		if checks == nil {
			// Unlike merge_readiness, a PR whose checks cannot be read is
			// still worth showing: it is yours, and you should know it exists.
			checks = noChecks()
		}
		extra := map[string]any{"draft": gh.Bool(it["draft"])}
		for k, val := range checks {
			extra[k] = val
		}
		return checked{row: Row(it, c.Now, extra), err: err}
	})

	rows, err := unpack(got)
	if err != nil {
		return nil, err
	}
	return SortByAge(Compact(rows)), nil
}
