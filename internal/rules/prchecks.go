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
    commits(last:1){nodes{commit{statusCheckRollup{ state
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
	if err != nil {
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
	if nodes := gh.List(gh.Map(pr["commits"])["nodes"]); len(nodes) > 0 {
		rollup = gh.Map(gh.Map(gh.Map(nodes[0])["commit"])["statusCheckRollup"])
	}

	var realFail, policyFail []string
	for _, raw := range gh.List(gh.Map(rollup["contexts"])["nodes"]) {
		ctx := gh.Map(raw)
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
