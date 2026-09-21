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
	items, err := c.Search("is:pr is:open author:" + c.Me)
	if err != nil {
		return nil, err
	}

	qa, ignore := v.Str("qa_label"), v.Strs("ignore_checks")
	rows := gh.PMap(items, c.Concurrency, func(it map[string]any) map[string]any {
		checks := prChecks(c, RepoName(it), Number(it), qa, ignore)
		if checks == nil {
			// Unlike merge_readiness, a PR whose checks cannot be read is
			// still worth showing: it is yours, and you should know it exists.
			checks = noChecks()
		}
		extra := map[string]any{"draft": gh.Bool(it["draft"])}
		for k, val := range checks {
			extra[k] = val
		}
		return Row(it, c.Now, extra)
	})

	return SortByAge(Compact(rows)), nil
}
