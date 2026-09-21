// Package rules holds every check Argus performs, one per file.
//
// A rule is a self-contained unit: it declares its own identity, its own
// tunable parameters and their defaults, and the function that produces
// its rows. It registers itself from init(), so adding a check means
// adding one file and nothing else - no central list to edit, no wiring.
//
// Every rule can be turned off and retuned without touching code:
//
//	ARGUS_RULE_STALE_PRS_ENABLED=false     # opt out entirely
//	ARGUS_RULE_STALE_PRS_DAYS=21           # retune a parameter
//
// The names come from the rule's own ID and parameter names, so whatever
// a contributor declares is configurable the moment they declare it, and
// `GET /api/rules` lists the lot - including anything added later - so
// the UI and the docs never drift from what the code actually does.
//
// See CONTRIBUTING.md for a worked example of writing one.
package rules

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Tzomily-Anvar/argus/internal/config"
)

// Param declares one tunable knob belonging to a rule. Default also
// fixes the type: an int default is parsed as an int, a []string default
// as a comma-separated list, and so on.
type Param struct {
	Name    string
	Desc    string
	Default any
}

// Rule is one check.
type Rule struct {
	// ID is the stable slug used in URLs, env var names and the UI.
	// Lowercase with underscores.
	ID string

	// Title is the human-readable heading.
	Title string

	// Description says what the rule surfaces, in one sentence.
	Description string

	// Why says why it is worth looking at, and what to do about a hit.
	// Shown in the UI as help text, so write it for the reader, not the
	// maintainer.
	Why string

	// Enabled is the default. Overridden by ARGUS_RULE_<ID>_ENABLED.
	Enabled bool

	// Params are this rule's knobs, if any.
	Params []Param

	// Run produces the rule's payload. Values carries the resolved
	// parameters, already merged with any overrides.
	Run func(c *Context, v Values) (any, error)
}

// Values holds a rule's resolved parameters.
type Values map[string]any

func (v Values) Int(name string) int {
	switch n := v[name].(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

func (v Values) Str(name string) string {
	s, _ := v[name].(string)
	return s
}

func (v Values) Bool(name string) bool {
	b, _ := v[name].(bool)
	return b
}

func (v Values) Strs(name string) []string {
	switch s := v[name].(type) {
	case []string:
		return s
	case string:
		var out []string
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	return nil
}

var (
	mu       sync.RWMutex
	registry = map[string]*Rule{}
)

// Register adds a rule. Called from init(); panics on a duplicate or
// malformed ID, because both are programming errors that should fail
// loudly at startup rather than silently shadow a rule.
func Register(r Rule) {
	mu.Lock()
	defer mu.Unlock()
	if r.ID == "" {
		panic("rules: a rule was registered without an ID")
	}
	if r.Run == nil {
		panic(fmt.Sprintf("rules: rule %q has no Run function", r.ID))
	}
	if _, dup := registry[r.ID]; dup {
		panic(fmt.Sprintf("rules: duplicate rule ID %q", r.ID))
	}
	registry[r.ID] = &r
}

// All returns every registered rule, ordered by ID for stable output.
func All() []*Rule {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns one rule by ID.
func Get(id string) (*Rule, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := registry[id]
	return r, ok
}

// Enabled returns the rules that are switched on right now.
func Enabled() []*Rule {
	var out []*Rule
	for _, r := range All() {
		if IsEnabled(r) {
			out = append(out, r)
		}
	}
	return out
}

func envKey(ruleID, param string) string {
	clean := strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(ruleID))
	if param == "" {
		return "ARGUS_RULE_" + clean + "_ENABLED"
	}
	return "ARGUS_RULE_" + clean + "_" + strings.ToUpper(param)
}

// IsEnabled reports whether a rule is switched on, honouring
// ARGUS_RULE_<ID>_ENABLED over the rule's own default.
func IsEnabled(r *Rule) bool {
	return config.Bool(envKey(r.ID, ""), r.Enabled)
}

// Resolve merges a rule's declared defaults with any environment
// overrides. The default's type decides how the override is parsed.
func Resolve(r *Rule) Values {
	v := Values{}
	for _, p := range r.Params {
		key := envKey(r.ID, p.Name)
		switch def := p.Default.(type) {
		case int:
			v[p.Name] = config.Int(key, def)
		case bool:
			v[p.Name] = config.Bool(key, def)
		case string:
			v[p.Name] = config.String(key, def)
		case []string:
			v[p.Name] = config.Strings(key, def)
		default:
			v[p.Name] = p.Default
		}
	}
	return v
}

// Describe renders a rule as JSON-friendly metadata, including the exact
// environment variable for each knob so the UI can tell a reader how to
// change it.
func Describe(r *Rule) map[string]any {
	params := make([]map[string]any, 0, len(r.Params))
	for _, p := range r.Params {
		params = append(params, map[string]any{
			"name":    p.Name,
			"desc":    p.Desc,
			"default": p.Default,
			"value":   Resolve(r)[p.Name],
			"env":     envKey(r.ID, p.Name),
		})
	}
	return map[string]any{
		"id":          r.ID,
		"title":       r.Title,
		"description": r.Description,
		"why":         r.Why,
		"enabled":     IsEnabled(r),
		"enabled_env": envKey(r.ID, ""),
		"params":      params,
	}
}
