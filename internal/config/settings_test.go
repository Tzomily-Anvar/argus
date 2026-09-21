package config

import (
	"strings"
	"testing"
)

// The catalogue has to be right about itself before it can be right about
// anything else: a default that its own validator rejects would have
// `argus config set` refuse the value the server is already using.
func TestEveryDefaultIsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Core() {
		if seen[s.Key] {
			t.Errorf("%s is in the catalogue twice", s.Key)
		}
		seen[s.Key] = true

		if s.Desc == "" {
			t.Errorf("%s has no description, so `argus config get` explains nothing", s.Key)
		}
		if s.Default == "" {
			continue
		}
		if err := s.Validate(s.Default); err != nil {
			t.Errorf("%s: its own default is invalid: %v", s.Key, err)
		}
	}
}

func TestValidation(t *testing.T) {
	cat := Core()
	cases := []struct {
		key   string
		value string
		ok    bool
	}{
		{"ARGUS_PORT", "8080", true},
		{"ARGUS_PORT", "eighty", false},
		{"ARGUS_PORT", "0", false},
		{"ARGUS_PORT", "70000", false},
		{"ARGUS_INCLUDE_ARCHIVED", "yes", true},
		{"ARGUS_INCLUDE_ARCHIVED", "maybe", false},
		{"ARGUS_SPRINT_HOURS_PER_POINT", "7.5", true},
		{"ARGUS_SPRINT_HOURS_PER_POINT", "a day", false},
		{"ARGUS_GITHUB_TOKEN_TYPE", "CLASSIC", true},
		{"ARGUS_GITHUB_TOKEN_TYPE", "personal", false},
		{"ARGUS_JIRA_BASE_URL", "https://example.atlassian.net", true},
		{"ARGUS_JIRA_BASE_URL", "example.atlassian.net", false},
		{"ARGUS_GITHUB_ORG", "your-org", true},
		{"ARGUS_GITHUB_ORG", "https://github.com/your-org", false},
		{"ARGUS_TOOLS", "pr,sprint", true},
		{"ARGUS_TOOLS", ",,", false},
		// A value that cannot survive the round trip through the file,
		// because the reader strips quotes from around one.
		{"ARGUS_JIRA_POINTS_FIELD_NAME", `"Story Points"`, false},
		{"ARGUS_JIRA_POINTS_FIELD_NAME", "Story Points", true},
		// Nothing may span a line: it would make the rest of the value a
		// setting of its own.
		{"ARGUS_BIND", "0.0.0.0\nARGUS_PORT=80", false},
		{"ARGUS_BIND", "", false},
	}
	for _, c := range cases {
		s, ok := cat.Find(c.key)
		if !ok {
			t.Fatalf("%s is not in the catalogue", c.key)
		}
		err := s.Validate(c.value)
		if c.ok && err != nil {
			t.Errorf("%s=%q should be accepted: %v", c.key, c.value, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s=%q should be refused", c.key, c.value)
		}
	}
}

func TestFindIgnoresCase(t *testing.T) {
	if _, ok := Core().Find("argus_port"); !ok {
		t.Error("shouting is not a spelling mistake")
	}
}

// A misspelt key is the failure this command exists to prevent, so the
// near miss has to be named.
func TestSuggest(t *testing.T) {
	cases := map[string]string{
		"ARGUS_GITHB_ORG":  "ARGUS_GITHUB_ORG",
		"ARGUS_PROT":       "ARGUS_PORT",
		"ARGUS_JIRA_EMIAL": "ARGUS_JIRA_EMAIL",
		"port":             "ARGUS_PORT",
	}
	for typed, want := range cases {
		got := Core().Suggest(typed)
		if len(got) == 0 || got[0] != want {
			t.Errorf("Suggest(%q) = %v, want %s first", typed, got, want)
		}
	}
	if got := Core().Suggest("COMPLETELY_UNRELATED"); len(got) != 0 {
		t.Errorf("Suggest of nonsense = %v, want nothing rather than a wrong guess", got)
	}
}

// Where a value came from is the question people actually arrive with.
func TestResolveOrder(t *testing.T) {
	s, _ := Core().Find("ARGUS_PORT")

	if r := s.Resolve(nil); r.Origin != FromDefault || r.Value != "18474" {
		t.Errorf("with nothing set: %s %q, want the default", r.Origin, r.Value)
	}

	file := map[string]string{"ARGUS_PORT": "9000"}
	if r := s.Resolve(file); r.Origin != FromFile || r.Value != "9000" {
		t.Errorf("with a file: %s %q, want the file's value", r.Origin, r.Value)
	}

	t.Setenv("ARGUS_PORT", "7000")
	r := s.Resolve(file)
	if r.Origin != FromEnv || r.Value != "7000" {
		t.Errorf("with both: %s %q, want the environment's value", r.Origin, r.Value)
	}
	if !r.Shadowed || r.InFile != "9000" {
		t.Error("a file value being overridden has to be reported, or the change looks like a no-op")
	}

	// An empty variable is not an answer. Compose declares variables that
	// the host never set, and letting one beat the file leaves someone
	// staring at a setting that is plainly there.
	t.Setenv("ARGUS_PORT", "  ")
	if r := s.Resolve(file); r.Origin != FromFile {
		t.Errorf("an empty environment variable won: %s", r.Origin)
	}
}

func TestSecretsAreMasked(t *testing.T) {
	s, _ := Core().Find("ARGUS_JIRA_TOKEN")
	got := s.Display("abcdefghijklmnop")
	if strings.Contains(got, "efghijklmnop") {
		t.Errorf("Display(...) = %q, which still shows the token", got)
	}
	if !strings.HasPrefix(got, "abcd") {
		t.Errorf("Display(...) = %q, want enough left to tell which token it is", got)
	}

	// A connection string's password is the secret; the host and database
	// are what you want to be able to check.
	db, _ := Core().Find("DATABASE_URL")
	got = db.Display("postgres://argus:hunter2@localhost:5432/argus")
	if strings.Contains(got, "hunter2") {
		t.Errorf("Display(...) = %q, which still shows the password", got)
	}
	if !strings.Contains(got, "localhost:5432") {
		t.Errorf("Display(...) = %q, want the host left legible", got)
	}
}

func TestPlainValuesAreNotMasked(t *testing.T) {
	s, _ := Core().Find("ARGUS_GITHUB_ORG")
	if got := s.Display("your-org"); got != "your-org" {
		t.Errorf("Display(%q) = %q", "your-org", got)
	}
}
