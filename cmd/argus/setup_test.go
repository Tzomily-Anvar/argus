package main

import "testing"

// The wizard's one required answer.
//
// It used to be checked against /orgs/{name}, which answers 404 for a
// person - so someone whose repositories live under their own profile
// was told their own username was not a real account. The name check
// itself never distinguished the two, and must not start to: GitHub's
// rule for a login is the same for both.
func TestAccountFromAnswer(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		want   string
		ok     bool
	}{
		{"an organisation", "your-org", "your-org", true},
		{"a person", "octocat", "octocat", true},
		{"a pasted profile address", "https://github.com/octocat", "octocat", true},
		{"a pasted organisation address", "https://github.com/orgs/your-org/repositories", "your-org", true},
		{"an address with a query string", "github.com/octocat?tab=repositories", "octocat", true},
		{"surrounding space", "  octocat  ", "octocat", true},
		{"an underscore, which GitHub does not allow", "octo_cat", "", false},
		{"a trailing hyphen", "octocat-", "", false},
		{"nothing at all", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := accountFromAnswer(tc.answer)
			if ok != tc.ok {
				t.Fatalf("accountFromAnswer(%q) accepted = %v, want %v", tc.answer, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("accountFromAnswer(%q) = %q, want %q", tc.answer, got, tc.want)
			}
		})
	}
}

func TestValidLogin(t *testing.T) {
	for _, s := range []string{"octocat", "your-org", "a", "a1-b2"} {
		if !validLogin(s) {
			t.Errorf("%q is a valid GitHub login", s)
		}
	}
	long := ""
	for range 40 {
		long += "a"
	}
	for _, s := range []string{"", "-lead", "trail-", "has space", "has.dot", long} {
		if validLogin(s) {
			t.Errorf("%q is not a valid GitHub login", s)
		}
	}
}
