package main

import (
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/rules"
)

// Rule settings are derived from the registry rather than written down
// again, which is what keeps a rule added next month configurable from
// the command line without anyone remembering a list.
func TestEveryRuleIsListed(t *testing.T) {
	all := settings()
	for _, r := range rules.All() {
		key := "ARGUS_RULE_" + strings.ToUpper(strings.ReplaceAll(r.ID, "-", "_")) + "_ENABLED"
		s, ok := all.Find(key)
		if !ok {
			t.Errorf("%s is not listed, so the rule cannot be switched off from here", key)
			continue
		}
		if s.Kind != config.KindBool {
			t.Errorf("%s is %s, want a true/false setting", key, s.Kind)
		}
		for _, p := range r.Params {
			pk := "ARGUS_RULE_" + strings.ToUpper(strings.ReplaceAll(r.ID, "-", "_")) +
				"_" + strings.ToUpper(p.Name)
			if _, ok := all.Find(pk); !ok {
				t.Errorf("%s declares %q but %s is not listed", r.ID, p.Name, pk)
			}
		}
	}
}

// A parameter's type comes from its default, the same way the rule
// registry decides how to parse an override. The two agreeing is what
// stops `argus config set` accepting a value the rule then ignores.
func TestRuleParameterTypes(t *testing.T) {
	cases := []struct {
		def  any
		kind config.Kind
		text string
	}{
		{14, config.KindInt, "14"},
		{true, config.KindBool, "true"},
		{[]string{"a", "b"}, config.KindList, "a,b"},
		{"x", config.KindString, "x"},
	}
	for _, c := range cases {
		kind, def := kindOf(c.def)
		if kind != c.kind || def != c.text {
			t.Errorf("kindOf(%v) = %s %q, want %s %q", c.def, kind, def, c.kind, c.text)
		}
	}
}

// A misspelt rule setting gets the same treatment as a misspelt core one:
// the whole point is that nothing unrecognised reaches the file.
func TestLookupSuggestsRuleSettings(t *testing.T) {
	_, err := lookup("ARGUS_RULE_STALE_PRS_DAY")
	if err == nil {
		t.Fatal("a key that is not a setting should be refused")
	}
	if !strings.Contains(err.Error(), "ARGUS_RULE_STALE_PRS_DAYS") {
		t.Errorf("error should name the near miss, got: %v", err)
	}
}
