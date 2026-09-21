package gh

import (
	"net/url"
	"strings"
	"sync"
)

// GitHub has two kinds of account, and Argus sweeps either one.
//
// The difference is not cosmetic. An organisation has endpoints a
// personal account simply does not have - /orgs/{org}/repos, the
// account-wide security alert feeds, teams - and the search API spells
// the same idea as org: in one case and user: in the other. Everywhere
// that has to choose between the two asks here.
//
// Nobody has to declare which they configured. GET /users/{login}
// answers for both kinds and its type field says which, so detection
// costs one request and ARGUS_GITHUB_ORG stays a single name.

// AccountKind is what GitHub reports in the type field. The constants
// carry GitHub's own spelling so the comparison is against the wire
// value rather than a translation of it.
type AccountKind string

const (
	AccountUnknown      AccountKind = ""
	AccountOrganisation AccountKind = "Organization"
	AccountUser         AccountKind = "User"
)

// Personal reports whether this is somebody's own account rather than an
// organisation.
func (k AccountKind) Personal() bool { return k == AccountUser }

// Label names the kind the way Argus's messages talk about it.
func (k AccountKind) Label() string {
	switch k {
	case AccountOrganisation:
		return "organisation"
	case AccountUser:
		return "personal account"
	}
	return "unknown"
}

// AccountKindOf reports whether a login belongs to an organisation or to
// a person.
//
// Memoised for the life of the client. An account does not change kind
// while Argus is running, and asking once rather than once per rule
// leaves the sweep's request budget to the rules themselves.
func (c *Client) AccountKindOf(login string) (AccountKind, error) {
	if strings.TrimSpace(login) == "" {
		return AccountUnknown, errf("no GitHub account was given to look up")
	}
	if kind, ok := c.cachedKind(login); ok {
		return kind, nil
	}

	// Deliberately outside the lock. This is a network call, and two
	// callers racing to make the same request costs far less than every
	// caller queueing behind whichever one got there first.
	payload, err := c.Get("/users/"+url.PathEscape(login), nil)
	if err != nil {
		return AccountUnknown, err
	}

	raw := Str(payload["type"])
	switch kind := AccountKind(raw); kind {
	case AccountOrganisation, AccountUser:
		c.rememberKind(login, kind)
		return kind, nil
	}
	if raw == "" {
		return AccountUnknown, errf("GitHub did not say what kind of account %q is", login)
	}
	// Bots and mannequins have logins too, and neither has repositories
	// to sweep. Saying what GitHub called it beats an empty dashboard.
	return AccountUnknown, errf(
		"GitHub says %q is a %s, which is neither an organisation nor a personal account. "+
			"ARGUS_GITHUB_ORG wants one of those.", login, raw)
}

// Logins are case-insensitive to GitHub, so the cache is keyed that way
// too: ARGUS_GITHUB_ORG and a login read back from the API often differ
// only in capitalisation.

func (c *Client) cachedKind(login string) (AccountKind, bool) {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	kind, ok := c.accounts[strings.ToLower(login)]
	return kind, ok
}

func (c *Client) rememberKind(login string, kind AccountKind) {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	if c.accounts == nil {
		c.accounts = map[string]AccountKind{}
	}
	c.accounts[strings.ToLower(login)] = kind
}

// accountCache is embedded in Client.
type accountCache struct {
	accountMu sync.Mutex
	accounts  map[string]AccountKind
}
