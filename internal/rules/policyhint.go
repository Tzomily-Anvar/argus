package rules

import (
	"sort"
	"strings"
)

// A check that fails on nearly every open pull request is not a broken
// build - it is a policy gate that has not been configured yet.
//
// This matters because of what a fresh install looks like. Argus cannot
// ship a default list of gate names: it would be guessing at someone
// else's CI, and a wrong guess hides a real failure. But without one,
// the first run of the dashboard reports almost everything as broken and
// nothing as mergeable, which is precisely backwards.
//
// So the data is left strictly truthful - a failure is still reported as
// a failure - and the shape of it is used to offer a suggestion instead.
// The user decides; the tool just points out what it noticed.

// policyHint is a check that looks like a gate rather than a test.
type policyHint struct {
	Name       string `json:"name"`
	Token      string `json:"token"`
	FailingOn  int    `json:"failing_on"`
	OutOf      int    `json:"out_of"`
	Env        string `json:"env"`
	Suggestion string `json:"suggestion"`
}

const (
	// A gate fails on nearly everything; a genuinely broken build does
	// not. Two thirds is high enough to avoid accusing a flaky test.
	hintRatio = 0.66
	// Below this there is not enough evidence to call it a pattern.
	hintMinPRs = 3
)

// detectPolicyHints looks for check names failing across a large share of
// the pull requests examined. Names already configured as gates are
// excluded, since those are exactly the ones the user has dealt with.
func detectPolicyHints(rows []map[string]any, ruleID string, configured []string) []policyHint {
	total := len(rows)
	if total < hintMinPRs {
		return nil
	}

	counts := map[string]int{}
	for _, r := range rows {
		failures, _ := r["real_failures"].([]string)
		// Count each name once per pull request, so a check that reports
		// several contexts on one PR does not look like a pattern.
		seen := map[string]bool{}
		for _, name := range failures {
			if seen[name] {
				continue
			}
			seen[name] = true
			counts[name]++
		}
	}

	var hints []policyHint
	for name, n := range counts {
		if float64(n)/float64(total) < hintRatio {
			continue
		}
		if matchesAny(name, configured) {
			continue
		}
		hints = append(hints, policyHint{
			Name:      name,
			Token:     hintToken(name),
			FailingOn: n,
			OutOf:     total,
			Env:       envKey(ruleID, "ignore_checks"),
			Suggestion: "This check fails on nearly every open pull request, which is the " +
				"signature of a policy gate rather than a broken build. Listing it will " +
				"report it separately instead of as a failure.",
		})
	}

	sort.SliceStable(hints, func(i, j int) bool { return hints[i].FailingOn > hints[j].FailingOn })
	return hints
}

// hintToken reduces a check name to the shortest thing worth putting in
// the ignore list. GitHub reports a status context as "workflow / job",
// which is awkward inside a comma-separated value and longer than it
// needs to be - matching is by substring, so the workflow name alone is
// enough and is what someone would naturally write.
//
// A name containing a comma cannot be shortened safely, because the
// value it goes into is comma-separated; it is returned unchanged and
// the reader can decide.
func hintToken(name string) string {
	if strings.Contains(name, ",") {
		return name
	}
	if before, _, found := strings.Cut(name, " / "); found && before != "" {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(name)
}
