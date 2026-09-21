package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every setting the code reads must appear in .env.example.
//
// Configuration that exists but is undocumented is configuration nobody
// can use: it was ARGUS_TOOLS going unmentioned that would have left a
// reader unable to switch the sprint report on at all. A test is the only
// thing that keeps a growing tool's documentation honest.
func TestEverySettingIsDocumented(t *testing.T) {
	root := "../.."

	// Credentials live in op.env, not .env.example.
	secrets := map[string]bool{
		"ARGUS_GITHUB_TOKEN": true,
		"ARGUS_JIRA_TOKEN":   true,
	}
	// Not user-facing: a prefix used to build rule variable names, and a
	// test-only database.
	internal := map[string]bool{
		"ARGUS_RULE_":             true,
		"ARGUS_TEST_DATABASE_URL": true,
		"ARGUS_JIRA_SPRINT":       true, // the live test's sprint selector
	}

	used := map[string]string{} // name -> file that reads it
	re := regexp.MustCompile(`"((?:ARGUS|DATABASE)_[A-Z_]+)"`)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "/web/") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatalf("reading .env.example: %v", err)
	}
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^#?\s*((?:ARGUS|DATABASE)_[A-Z_]+)\s*=`).
		FindAllStringSubmatch(string(b), -1) {
		documented[m[1]] = true
	}

	for name, file := range used {
		if secrets[name] || internal[name] || documented[name] {
			continue
		}
		t.Errorf("%s is read by %s but not documented in .env.example", name, file)
	}
}
