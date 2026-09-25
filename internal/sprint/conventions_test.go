package sprint_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The catalogue, the settings and the documentation page are three
// statements of the same thing, and this holds them together.
//
// A convention that is catalogued but not documented is one nobody can
// look up when a number looks odd; a sprint setting that is not
// catalogued is a convention nobody has stated out loud, which is how a
// wrong one goes unnoticed. So a setting added to the Jira or sprint
// sections has to be named here and explained on the page before the
// build passes.

// Sections of config.Core() whose every setting must be catalogued.
var catalogued = map[string]bool{"Jira": true, "Sprint report": true}

var hows = map[string]bool{
	sprint.HowDeclared: true, sprint.HowAsked: true, sprint.HowSetting: true,
	sprint.HowFixed: true, sprint.HowApp: true,
}

func TestEveryConventionIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range sprint.Conventions {
		if c.Key == "" || c.Summary == "" {
			t.Errorf("convention %+v needs a key and a summary", c)
		}
		if seen[c.Key] {
			t.Errorf("convention %q is catalogued twice", c.Key)
		}
		seen[c.Key] = true
		if !hows[c.How] {
			t.Errorf("convention %q: How %q is not one of the How constants", c.Key, c.How)
		}
		if c.How == sprint.HowDeclared && c.Source == "" {
			t.Errorf("convention %q is declared by Jira but names no Source", c.Key)
		}
		if !strings.HasPrefix(c.Doc, "#") {
			t.Errorf("convention %q: Doc %q is not an anchor", c.Key, c.Doc)
		}
	}
}

func TestEveryConventionSettingExists(t *testing.T) {
	core := config.Core()
	for _, c := range sprint.Conventions {
		if c.Setting == "" {
			continue
		}
		if _, ok := core.Find(c.Setting); !ok {
			t.Errorf("convention %q names %s, which is not in config.Core()", c.Key, c.Setting)
		}
	}
}

func TestEverySprintSettingIsCatalogued(t *testing.T) {
	named := map[string]int{}
	for _, c := range sprint.Conventions {
		if c.Setting != "" {
			named[c.Setting]++
		}
	}
	for _, s := range config.Core() {
		if !catalogued[s.Section] {
			continue
		}
		switch named[s.Key] {
		case 0:
			t.Errorf("%s is a %s setting and no convention names it: add it to "+
				"internal/sprint/conventions.go and to docs/sprint-conventions.md", s.Key, s.Section)
		case 1:
		default:
			t.Errorf("%s is named by %d conventions; a setting belongs to one", s.Key, named[s.Key])
		}
	}
}

func TestEveryConventionIsDocumented(t *testing.T) {
	// The page is read from the repository root, the way the config
	// package's documentation test finds .env.example.
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "sprint-conventions.md"))
	if err != nil {
		t.Fatalf("reading docs/sprint-conventions.md: %v", err)
	}
	anchors := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`).FindAllStringSubmatch(string(b), -1) {
		anchors[anchor(m[1])] = true
	}
	for _, c := range sprint.Conventions {
		if !anchors[c.Doc] {
			t.Errorf("convention %q points at %s, which is not a heading in docs/sprint-conventions.md",
				c.Key, c.Doc)
		}
	}
}

// anchor turns a heading into the fragment a Markdown renderer gives it:
// lowercased, punctuation dropped, spaces to hyphens.
func anchor(heading string) string {
	var b strings.Builder
	b.WriteByte('#')
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}
