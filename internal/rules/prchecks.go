package rules

import (
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

const prChecksQuery = `
query($owner:String!,$name:String!,$number:Int!){
  repository(owner:$owner,name:$name){ pullRequest(number:$number){
    reviewDecision
    labels(first:30){nodes{name}}
    commits(last:1){nodes{commit{oid statusCheckRollup{ state
      contexts(first:100){nodes{
        __typename
        ... on CheckRun{name conclusion}
        ... on StatusContext{context state}
      }}
    }}}}
  }}}`

// failedConclusions are the check states that mean "this did not pass".
var failedConclusions = map[string]bool{
	"FAILURE": true, "ERROR": true, "TIMED_OUT": true,
	"ACTION_REQUIRED": true, "CANCELLED": true,
}

// prChecks fetches a pull request's review decision, labels and check
// results, and splits failures into two kinds.
//
// The distinction matters more than it looks. Many organisations have a
// required check that is a policy gate rather than a test - "this PR is
// missing a required label", say. It fails red exactly like a broken
// build, so an approved, working PR can look broken when the only thing
// wrong is a missing label. Names matching ignoreChecks are reported
// separately as policy failures, leaving real_failures to mean what it
// says: something is actually broken.
//
// Returns nil if the PR could not be read; callers decide whether that is
// fatal or just a row without check data.
func prChecks(c *Context, repo string, number int, qaLabel string, ignoreChecks []string) map[string]any {
	data, err := c.Client.GraphQL(prChecksQuery, map[string]any{
		"owner": c.Org, "name": repo, "number": number,
	})
	// Partial data is still worth having: the review decision and labels
	// usually resolve even when the check contexts do not, and a pull
	// request with a known review state and unknown checks is far more
	// useful than no row at all.
	if err != nil && !gh.IsPartial(err) {
		return nil
	}
	pr := gh.Map(gh.Map(data["repository"])["pullRequest"])
	if pr == nil {
		return nil
	}

	var labels []string
	for _, n := range gh.List(gh.Map(pr["labels"])["nodes"]) {
		if name := gh.Str(gh.Map(n)["name"]); name != "" {
			labels = append(labels, name)
		}
	}

	var rollup map[string]any
	var headSHA string
	if nodes := gh.List(gh.Map(pr["commits"])["nodes"]); len(nodes) > 0 {
		commit := gh.Map(gh.Map(nodes[0])["commit"])
		headSHA = gh.Str(commit["oid"])
		rollup = gh.Map(commit["statusCheckRollup"])
	}

	// A token without the Checks API still gets the rollup and its overall
	// state, but every context node comes back as null - the checks are
	// there and individually refused. Counting both tells the difference
	// between "no checks ran" and "checks ran and cannot be read".
	var realFail, policyFail []string
	nodes := gh.List(gh.Map(rollup["contexts"])["nodes"])
	readable := 0
	for _, raw := range nodes {
		ctx := gh.Map(raw)
		if ctx == nil {
			continue
		}
		readable++
		name := gh.Str(ctx["name"])
		if name == "" {
			name = gh.Str(ctx["context"])
		}
		state := strings.ToUpper(gh.Str(ctx["conclusion"]))
		if state == "" {
			state = strings.ToUpper(gh.Str(ctx["state"]))
		}
		if !failedConclusions[state] {
			continue
		}
		if matchesAny(name, ignoreChecks) {
			policyFail = append(policyFail, name)
		} else {
			realFail = append(realFail, name)
		}
	}

	// Checks ran but none could be read, so fall back to workflow runs -
	// a different API behind Actions: Read, which a fine-grained token can
	// hold. It misses check runs posted by other apps, but it carries the
	// conclusion of the team's own CI, which is the question being asked.
	checksFrom := "check runs"
	if readable == 0 && len(nodes) > 0 {
		if runs, err := workflowRunsFor(c, repo, headSHA); err == nil && len(runs) > 0 {
			realFail, policyFail = checksFromWorkflowRuns(runs, ignoreChecks)
			checksFrom = "workflow runs"
		}
	}

	decision := gh.Str(pr["reviewDecision"])
	if decision == "" {
		decision = "NONE"
	}

	return map[string]any{
		"review_decision": decision,
		"has_qa_label":    qaLabel != "" && contains(labels, qaLabel),
		"qa_label_used":   qaLabel != "",
		"labels":          strs(labels),
		"real_failures":   strs(realFail),
		"policy_failures": strs(policyFail),
		"checks_green":    len(realFail) == 0,
		"checks_from":     checksFrom,
	}
}

// noChecks is the placeholder used when a PR's checks could not be read,
// so a row still renders instead of vanishing.
func noChecks() map[string]any {
	return map[string]any{
		"review_decision": "NONE",
		"has_qa_label":    false,
		"qa_label_used":   false,
		"labels":          []string{},
		"real_failures":   []string{},
		"policy_failures": []string{},
		"checks_green":    true,
	}
}

func matchesAny(name string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(name, n) {
			return true
		}
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// checkParams are shared by every rule that reports check status, so the
// two knobs are declared identically wherever they appear.
func checkParams() []Param {
	return []Param{
		{
			Name:    "qa_label",
			Desc:    "A label that gates merge in your workflow, if you use one. Leave empty to disable the check.",
			Default: "",
		},
		{
			Name: "ignore_checks",
			Desc: "Check names that are policy gates rather than real CI, comma separated. " +
				"Matching failures are reported separately so they do not read as broken builds.",
			Default: []string{},
		},
	}
}
