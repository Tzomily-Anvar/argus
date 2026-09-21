package rules

import (
	"strings"
	"testing"
)

// Every rule is part of the public surface: its ID appears in env var
// names and the API, and its help text is shown to readers. These checks
// keep a newly contributed rule honest.

func TestEveryRuleIsWellFormed(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no rules registered")
	}
	for _, r := range all {
		if r.ID != strings.ToLower(r.ID) || strings.ContainsAny(r.ID, " -") {
			t.Errorf("rule %q: ID must be lowercase with underscores", r.ID)
		}
		if r.Title == "" || r.Description == "" || r.Why == "" {
			t.Errorf("rule %q: Title, Description and Why are all required", r.ID)
		}
		if r.Run == nil {
			t.Errorf("rule %q: no Run function", r.ID)
		}
		seen := map[string]bool{}
		for _, p := range r.Params {
			if p.Name == "" || p.Desc == "" {
				t.Errorf("rule %q: every param needs a Name and Desc", r.ID)
			}
			if seen[p.Name] {
				t.Errorf("rule %q: duplicate param %q", r.ID, p.Name)
			}
			seen[p.Name] = true
			switch p.Default.(type) {
			case int, bool, string, []string:
			default:
				t.Errorf("rule %q param %q: unsupported default type %T", r.ID, p.Name, p.Default)
			}
		}
	}
}

func TestResolveUsesDeclaredDefaults(t *testing.T) {
	for _, r := range All() {
		v := Resolve(r)
		for _, p := range r.Params {
			if _, ok := v[p.Name]; !ok {
				t.Errorf("rule %q: Resolve dropped param %q", r.ID, p.Name)
			}
		}
	}
}

func TestEnvKeyNaming(t *testing.T) {
	if got := envKey("stale_prs", ""); got != "ARGUS_RULE_STALE_PRS_ENABLED" {
		t.Errorf("envKey enabled = %q", got)
	}
	if got := envKey("stale_prs", "days"); got != "ARGUS_RULE_STALE_PRS_DAYS" {
		t.Errorf("envKey param = %q", got)
	}
}

// Describe feeds the UI and the docs; if it drops a param, a knob becomes
// invisible to anyone who did not read the source.
func TestDescribeExposesEveryParam(t *testing.T) {
	for _, r := range All() {
		d := Describe(r)
		params, _ := d["params"].([]map[string]any)
		if len(params) != len(r.Params) {
			t.Errorf("rule %q: Describe returned %d params, rule declares %d",
				r.ID, len(params), len(r.Params))
		}
		for _, p := range params {
			if p["env"] == "" {
				t.Errorf("rule %q: param %v has no env var name", r.ID, p["name"])
			}
		}
	}
}
