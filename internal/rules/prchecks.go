package rules

import (
	"slices"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// One query for everything any rule wants about a pull request.
//
// reviewRequests is here for the unreviewed rule, which used to fetch the
// whole pull request over REST for nothing but this count. It costs a
// field on a query two other rules were already making.
//
// reviewThreads is for the "unresolved" badge: a review comment nobody
// has answered or resolved is work the author owes, and GitHub shows it
// only inside the conversation tab. A hundred threads is more than any
// pull request should have; past that the count reads as "100+".
//
// latestOpinionatedReviews is each reviewer's latest approval or request
// for changes, and the base and default branches say whether the pull
// request merges where the rulesets are: see approvalState.
//
// mine is the viewer's own reviews, comments included, with the commit
// each was left on: see myReview. latestReviews would be one field
// fewer, but it leaves out a reviewer who has only ever commented.
const pullRequestQuery = `
query($owner:String!,$name:String!,$number:Int!,$me:String){
  repository(owner:$owner,name:$name){ pullRequest(number:$number){
    reviewDecision
    baseRefName
    baseRepository{defaultBranchRef{name}}
    latestOpinionatedReviews(first:100){nodes{state}}
    mine: reviews(last:30,author:$me){nodes{state commit{oid}}}
    reviewRequests(first:1){totalCount}
    reviewThreads(first:100){nodes{isResolved}}
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
// Returns nil data if the pull request could not be read, alongside the
// reason. Callers decide whether that is fatal or just a row without
// check data - but a rate limit is never "just a row": see the rules.
func prChecks(c *Context, repo string, number int, qaLabel string, ignoreChecks []string) (map[string]any, error) {
	pr, err := c.PullRequest(repo, number)
	if err != nil || pr == nil {
		return nil, err
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

	approvals := 0
	for _, raw := range gh.List(gh.Map(pr["latestOpinionatedReviews"])["nodes"]) {
		if gh.Str(gh.Map(raw)["state"]) == "APPROVED" {
			approvals++
		}
	}
	base := gh.Str(pr["baseRefName"])
	defaultBranch := gh.Str(gh.Map(gh.Map(pr["baseRepository"])["defaultBranchRef"])["name"])
	required := 0
	if defaultBranch != "" {
		required = c.RequiredApprovals(repo, defaultBranch)
	}
	decision := approvalState(gh.Str(pr["reviewDecision"]), approvals, required)
	// The base is named only where it is not the default branch: that is
	// the case a reader needs told about.
	if base == defaultBranch {
		base = ""
	}

	mine, onCommit := "", ""
	if c.Me != "" {
		mine, onCommit = myReview(pr)
	}

	return map[string]any{
		"review_decision":    decision,
		"github_decision":    gh.Str(pr["reviewDecision"]),
		"approvals":          approvals,
		"approvals_required": required,
		"base_branch":        base,
		"unresolved_threads": unresolvedThreads(pr),
		"has_qa_label":       qaLabel != "" && slices.Contains(labels, qaLabel),
		"qa_label_used":      qaLabel != "",
		"labels":             strs(labels),
		"real_failures":      strs(realFail),
		"policy_failures":    strs(policyFail),
		"checks_green":       len(realFail) == 0,
		"checks_from":        checksFrom,
		"head_sha":           headSHA,
		"my_review":          mine,
		"my_review_current":  mine != "" && onCommit == headSHA,
	}, nil
}

// myReview is what the viewer last said on a pull request and the commit
// they last said anything on. The say is their latest approval or
// request for changes, as GitHub counts it, or COMMENTED when they have
// only ever commented; a reply in a review thread is a review of its own
// to GitHub, so it counts. The commit is the one the latest review of
// any kind was left on: a comment after an approval is still a look at
// the code as it then stood. A review not yet submitted is nobody's to
// read. Empty when the viewer has not reviewed it.
func myReview(pr map[string]any) (state, commit string) {
	for _, raw := range gh.List(gh.Map(pr["mine"])["nodes"]) {
		r := gh.Map(raw)
		s := gh.Str(r["state"])
		if s == "PENDING" || s == "" {
			continue
		}
		commit = gh.Str(gh.Map(r["commit"])["oid"])
		switch s {
		case "APPROVED", "CHANGES_REQUESTED":
			state = s
		default:
			if state == "" {
				state = "COMMENTED"
			}
		}
	}
	return state, commit
}

// approvalState is the review decision to report: GitHub's, unless it
// says approved on fewer approvals than the default branch asks for.
//
// GitHub decides against the branch a pull request merges into. One
// stacked on a feature branch the rulesets do not cover is approved on a
// single review, though its work reaches the default branch only by a
// merge the full count is asked of. Reading it as still needing review
// keeps it out of Ready to merge until it has the approvals the team's
// rule asks for. A change requested is never overruled, and no count
// asked for, or none readable, leaves GitHub's word as it is.
func approvalState(github string, approvals, required int) string {
	if github == "" {
		github = "NONE"
	}
	if github == "APPROVED" && required > 0 && approvals < required {
		return "REVIEW_REQUIRED"
	}
	return github
}

// unresolvedThreads counts the review threads nobody has resolved.
func unresolvedThreads(pr map[string]any) int {
	n := 0
	for _, raw := range gh.List(gh.Map(pr["reviewThreads"])["nodes"]) {
		if t := gh.Map(raw); t != nil && !gh.Bool(t["isResolved"]) {
			n++
		}
	}
	return n
}

// ReviewRequested reports whether anyone - a person or a team - has been
// asked to review. GraphQL's reviewRequests covers both, where REST needs
// requested_reviewers and requested_teams read separately.
func reviewRequested(pr map[string]any) bool {
	return gh.Num(gh.Map(pr["reviewRequests"])["totalCount"]) > 0
}

// noChecks is the placeholder used when a PR's checks could not be read,
// so a row still renders instead of vanishing.
func noChecks() map[string]any {
	return map[string]any{
		"review_decision":    "NONE",
		"github_decision":    "",
		"approvals":          0,
		"approvals_required": 0,
		"base_branch":        "",
		"unresolved_threads": 0,
		"has_qa_label":       false,
		"qa_label_used":      false,
		"labels":             []string{},
		"real_failures":      []string{},
		"policy_failures":    []string{},
		"checks_green":       true,
		"head_sha":           "",
		"my_review":          "",
		"my_review_current":  false,
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
