package rules

import (
	"fmt"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// CI status without the Checks API.
//
// GitHub grants the Checks API only to GitHub Apps, so a fine-grained
// token gets 403 on check runs and check suites, and the GraphQL
// statusCheckRollup comes back FORBIDDEN. That would leave every pull
// request with unknown CI status.
//
// Workflow runs are a different API behind a different permission -
// Actions: Read - and a fine-grained token can read them. They are not a
// perfect substitute: they cover workflows this repository runs, and miss
// check runs posted by other GitHub Apps such as CodeQL. But they carry
// the conclusion of the team's own CI, which is what the report is
// actually asking about.
//
// The names differ too. A check run is named for its job
// ("lint / lint"); a workflow run is named for its workflow ("Lint").
// Anyone matching policy gates by name needs both
// spellings in their ignore list, which the report says plainly rather
// than leaving to be discovered.

type workflowRun struct {
	Name       string
	Conclusion string
}

// workflowRunsFor returns the workflow conclusions for a commit.
func workflowRunsFor(c *Context, repo, sha string) ([]workflowRun, error) {
	if sha == "" {
		return nil, nil
	}
	payload, err := c.Client.Get(
		fmt.Sprintf("/repos/%s/%s/actions/runs", c.Org, repo),
		Params("head_sha", sha, "per_page", "50"),
	)
	if err != nil {
		return nil, err
	}

	var out []workflowRun
	for _, raw := range gh.List(payload["workflow_runs"]) {
		run := gh.Map(raw)
		out = append(out, workflowRun{
			Name:       gh.Str(run["name"]),
			Conclusion: strings.ToUpper(gh.Str(run["conclusion"])),
		})
	}
	return out, nil
}

// checksFromWorkflowRuns builds the same shape prChecks returns, from
// workflow conclusions rather than check runs.
func checksFromWorkflowRuns(runs []workflowRun, ignore []string) (real, policy []string) {
	for _, r := range runs {
		if !failedConclusions[r.Conclusion] {
			continue
		}
		if matchesAny(r.Name, ignore) {
			policy = append(policy, r.Name)
		} else {
			real = append(real, r.Name)
		}
	}
	return real, policy
}
