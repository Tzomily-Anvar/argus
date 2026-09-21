package rules

import "testing"

func TestWholeOrgByDefault(t *testing.T) {
	s := Scope{Org: "acme"}
	if got := s.Query(); got != "org:acme archived:false" {
		t.Errorf("Query() = %q", got)
	}
	if !s.Allows("anything") {
		t.Error("an empty allowlist must allow every repository")
	}
}

func TestArchivedIncludedOnRequest(t *testing.T) {
	s := Scope{Org: "acme", Archived: true}
	if got := s.Query(); got != "org:acme" {
		t.Errorf("Query() = %q, want no archived qualifier", got)
	}
}

func TestAllowlistBecomesRepoQualifiers(t *testing.T) {
	s := Scope{Org: "acme", Only: []string{"api", "web"}}
	want := "repo:acme/api repo:acme/web archived:false"
	if got := s.Query(); got != want {
		t.Errorf("Query() = %q, want %q", got, want)
	}
	if !s.Allows("api") || s.Allows("other") {
		t.Error("allowlist must admit only its own members")
	}
}

func TestExclusionsSubtractFromTheOrg(t *testing.T) {
	s := Scope{Org: "acme", Excluded: []string{"sandbox"}}
	want := "org:acme -repo:acme/sandbox archived:false"
	if got := s.Query(); got != want {
		t.Errorf("Query() = %q, want %q", got, want)
	}
	if s.Allows("sandbox") {
		t.Error("an excluded repository must not be allowed")
	}
}

// An exclusion should not be defeated by casing, since these names are
// typed by hand into a .env file.
func TestMatchingIgnoresCase(t *testing.T) {
	s := Scope{Org: "acme", Excluded: []string{"Sandbox"}}
	if s.Allows("sandbox") || s.Allows("SANDBOX") {
		t.Error("repository matching must ignore case")
	}
}

// GitHub rejects an over-long search query, so a large allowlist is
// truncated rather than producing one.
func TestLongAllowlistIsTruncated(t *testing.T) {
	many := make([]string, 60)
	for i := range many {
		many[i] = "repository-with-a-fairly-long-name"
	}
	s := Scope{Org: "acme", Only: many}
	if got := s.Query(); len(got) > 256 {
		t.Errorf("query is %d characters; GitHub caps search at 256", len(got))
	}
}

func TestFilterDropsOutOfScopeRows(t *testing.T) {
	s := Scope{Org: "acme", Excluded: []string{"sandbox"}}
	rows := []map[string]any{{"repo": "api"}, {"repo": "sandbox"}, {"repo": "web"}}
	got := s.Filter(rows, "repo")
	if len(got) != 2 || got[0]["repo"] != "api" || got[1]["repo"] != "web" {
		t.Errorf("Filter() = %v", got)
	}
}
