package config

import "testing"

// The wizard's one required answer.
//
// It used to be checked against /orgs/{name}, which answers 404 for a
// person - so someone whose repositories live under their own profile
// was told their own username was not a real account. The name check
// itself never distinguished the two, and must not start to: GitHub's
// rule for a login is the same for both.
func TestAccountName(t *testing.T) {
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
			got, ok := AccountName(tc.answer)
			if ok != tc.ok {
				t.Fatalf("AccountName(%q) accepted = %v, want %v", tc.answer, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("AccountName(%q) = %q, want %q", tc.answer, got, tc.want)
			}
		})
	}
}

func TestValidLogin(t *testing.T) {
	for _, s := range []string{"octocat", "your-org", "a", "a1-b2"} {
		if !ValidLogin(s) {
			t.Errorf("%q is a valid GitHub login", s)
		}
	}
	long := ""
	for range 40 {
		long += "a"
	}
	for _, s := range []string{"", "-lead", "trail-", "has space", "has.dot", long} {
		if ValidLogin(s) {
			t.Errorf("%q is not a valid GitHub login", s)
		}
	}
}

// The setting itself is read the same way, so a file edited by hand with
// the address in it still names the account.
func TestOrgReadsAnAddress(t *testing.T) {
	for raw, want := range map[string]string{
		"acme-widgets": "acme-widgets",
		"https://github.com/orgs/acme-widgets/repositories": "acme-widgets",
		"github.com/octocat": "octocat",
	} {
		t.Setenv("ARGUS_GITHUB_ORG", raw)
		got, err := Org()
		if err != nil || got != want {
			t.Errorf("Org() with %q = %q, %v; want %q", raw, got, err, want)
		}
	}
	t.Setenv("ARGUS_GITHUB_ORG", "acme widgets")
	if _, err := Org(); err == nil {
		t.Error("a value with a space should be refused")
	}
	t.Setenv("ARGUS_GITHUB_ORG", "")
	if _, err := Org(); err == nil {
		t.Error("an empty value is missing")
	}
}
