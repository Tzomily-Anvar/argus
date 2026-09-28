package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The registry is what lets `argus config list` say a value came from
// Jira and which read it came from. It is a file so that a separate
// process can read it, and a person's own setting always beats it.

func isolatedRegistry(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "declared.json")
	UseDeclaredFile(path)
	t.Cleanup(func() { UseDeclaredFile("") })
	return path
}

func TestDeclaredValueResolvesFromJiraUnlessOverridden(t *testing.T) {
	isolatedRegistry(t)
	s, _ := Core().Find("ARGUS_JIRA_DONE_STATUSES")
	t.Setenv("ARGUS_JIRA_DONE_STATUSES", "")

	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	Declared(s.Key, "Done, Ready for production", "status categories", at)

	r := s.Resolve(nil)
	if r.Origin != FromJira || r.Value != "Done, Ready for production" {
		t.Fatalf("resolved %q from %s, want the declared value from Jira", r.Value, r.Origin)
	}
	if r.Declared == nil || r.Declared.Source != "status categories" || !r.Declared.At.Equal(at) {
		t.Errorf("declaration = %+v, want the source and date of the read", r.Declared)
	}

	// The file is the person's, and the person wins.
	if r := s.Resolve(map[string]string{s.Key: "Done"}); r.Origin != FromFile || r.Value != "Done" {
		t.Errorf("with the file set: %q from %s, want the file's value", r.Value, r.Origin)
	}
	t.Setenv("ARGUS_JIRA_DONE_STATUSES", "Closed")
	if r := s.Resolve(nil); r.Origin != FromEnv || r.Value != "Closed" {
		t.Errorf("with the environment set: %q from %s, want the environment's value", r.Value, r.Origin)
	}
}

func TestDeclaredValuesSurviveAReload(t *testing.T) {
	path := isolatedRegistry(t)
	s, _ := Core().Find("ARGUS_JIRA_POINTS_FIELD")
	t.Setenv("ARGUS_JIRA_POINTS_FIELD", "")

	Declared(s.Key, "customfield_10016", "board estimation", time.Now())
	DeclaredStale(s.Key)

	// A second process starts with nothing in memory and reads the file.
	UseDeclaredFile(path)
	d, ok := s.Declaration()
	if !ok || d.Value != "customfield_10016" || !d.Stale {
		t.Errorf("after a reload: %+v, want the declared value marked stale", d)
	}
	if r := s.Resolve(nil); r.Origin != FromJira {
		t.Errorf("origin = %s, want %s", r.Origin, FromJira)
	}
}

// A setting read under a former name still works, and says so once.
func TestFormerNamesAreReadWithANotice(t *testing.T) {
	t.Setenv("ARGUS_TEST_NEW_NAME", "")
	t.Setenv("ARGUS_TEST_OLD_NAME", "from the old key")
	cat := Catalogue{{Key: "ARGUS_TEST_NEW_NAME", Was: []string{"ARGUS_TEST_OLD_NAME"}}}

	notices := FormerNames(cat)
	if len(notices) != 1 || !strings.Contains(notices[0], "ARGUS_TEST_OLD_NAME") ||
		!strings.Contains(notices[0], "ARGUS_TEST_NEW_NAME") {
		t.Fatalf("notices = %q, want one naming both keys", notices)
	}
	if got := String("ARGUS_TEST_NEW_NAME", ""); got != "from the old key" {
		t.Errorf("the new key reads %q, want the old key's value", got)
	}

	// The current name set, the old one is left alone and nothing is said.
	t.Setenv("ARGUS_TEST_NEW_NAME", "current")
	if notices := FormerNames(cat); len(notices) != 0 {
		t.Errorf("with the current key set, notices = %q, want none", notices)
	}
	if got := String("ARGUS_TEST_NEW_NAME", ""); got != "current" {
		t.Errorf("the new key reads %q, want its own value", got)
	}
}
