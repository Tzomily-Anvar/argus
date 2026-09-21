package rules

import "testing"

func prs(failuresPerPR ...[]string) []map[string]any {
	rows := make([]map[string]any, 0, len(failuresPerPR))
	for _, f := range failuresPerPR {
		rows = append(rows, map[string]any{"real_failures": f})
	}
	return rows
}

func TestFlagsACheckFailingOnNearlyEverything(t *testing.T) {
	rows := prs(
		[]string{"needs-label"}, []string{"needs-label"}, []string{"needs-label"},
		[]string{"needs-label"}, []string{},
	)
	hints := detectPolicyHints(rows, "merge_readiness", nil)
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}
	if hints[0].Name != "needs-label" || hints[0].FailingOn != 4 || hints[0].OutOf != 5 {
		t.Errorf("unexpected hint: %+v", hints[0])
	}
	if hints[0].Env != "ARGUS_RULE_MERGE_READINESS_IGNORE_CHECKS" {
		t.Errorf("hint should name the env var to set, got %q", hints[0].Env)
	}
}

// The whole point is to avoid accusing a genuinely failing test, so a
// failure on a minority of pull requests must stay a failure.
func TestDoesNotFlagAnOrdinaryFailure(t *testing.T) {
	rows := prs(
		[]string{"unit tests"}, []string{}, []string{}, []string{}, []string{},
	)
	if hints := detectPolicyHints(rows, "merge_readiness", nil); len(hints) != 0 {
		t.Errorf("a failure on 1 of 5 PRs must not be called a policy gate: %+v", hints)
	}
}

// Once configured, the suggestion has been acted on and must stop.
func TestDoesNotRepeatAConfiguredGate(t *testing.T) {
	rows := prs([]string{"needs-label"}, []string{"needs-label"}, []string{"needs-label"})
	if hints := detectPolicyHints(rows, "merge_readiness", []string{"needs-label"}); len(hints) != 0 {
		t.Errorf("configured gate should not be suggested again: %+v", hints)
	}
}

func TestNeedsEnoughEvidence(t *testing.T) {
	rows := prs([]string{"needs-label"}, []string{"needs-label"})
	if hints := detectPolicyHints(rows, "merge_readiness", nil); len(hints) != 0 {
		t.Errorf("two pull requests is not a pattern: %+v", hints)
	}
}

// A check reporting several contexts on one pull request should count
// once, not once per context, or a single noisy PR could look like a gate.
func TestCountsEachPullRequestOnce(t *testing.T) {
	rows := prs(
		[]string{"gate", "gate", "gate"}, []string{}, []string{}, []string{},
	)
	if hints := detectPolicyHints(rows, "merge_readiness", nil); len(hints) != 0 {
		t.Errorf("repeats within one PR must not count as a pattern: %+v", hints)
	}
}

func TestHintTokenShortensAContextName(t *testing.T) {
	cases := map[string]string{
		"lint / lint":                  "lint",
		"build / test (ubuntu-latest)": "build",
		"needs-label":                  "needs-label",
		"  spaced  ":                   "spaced",
		// A comma would corrupt the comma-separated value it goes into.
		"weird,name / job": "weird,name / job",
	}
	for in, want := range cases {
		if got := hintToken(in); got != want {
			t.Errorf("hintToken(%q) = %q, want %q", in, got, want)
		}
	}
}
