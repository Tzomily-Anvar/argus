package rules

import "strings"

// Repository scoping, resolved once and applied everywhere.
//
// Three different mechanisms need it and they cannot share an
// implementation, so they share this instead:
//
//   - issue search takes qualifiers in the query string;
//   - the repository listing is filtered after the fact;
//   - the account-wide alert endpoints cannot be scoped at all, so their
//     results are filtered by repository name on the way out.
//
// Keeping the decision here means "which repositories does Argus look
// at" has one answer rather than three that can drift.

// Scope is the resolved repository selection.
type Scope struct {
	Org      string
	Personal bool     // the account is somebody's own, not an organisation
	Only     []string // allowlist; empty means every repository in the account
	Excluded []string
	Archived bool // include archived repositories
}

// Query returns the search qualifiers that scope a search to this
// selection. An allowlist becomes a set of repo: qualifiers, which
// GitHub treats as alternatives; otherwise the account is named and
// exclusions are subtracted.
//
// Naming the account is the one qualifier that differs between the two
// kinds: search spells it org: for an organisation and user: for a
// personal account, and using the wrong one matches nothing at all
// rather than failing. repo: is the same word for both.
//
// GitHub caps search queries at 256 characters, so a long allowlist is
// truncated rather than silently producing a malformed query. Anyone
// with that many repositories wants an exclusion list instead.
func (s Scope) Query() string {
	var b strings.Builder
	if len(s.Only) > 0 {
		for i, r := range s.Only {
			if b.Len() > 180 {
				break
			}
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString("repo:" + s.Org + "/" + r)
		}
	} else {
		b.WriteString(s.ownerQualifier() + ":" + s.Org)
		for _, r := range s.Excluded {
			if b.Len() > 180 {
				break
			}
			b.WriteString(" -repo:" + s.Org + "/" + r)
		}
	}
	if !s.Archived {
		b.WriteString(" archived:false")
	}
	return b.String()
}

func (s Scope) ownerQualifier() string {
	if s.Personal {
		return "user"
	}
	return "org"
}

// Allows reports whether a repository is in scope. Used where a query
// cannot express the selection - the account-wide alert endpoints return
// everything the token can see, with no way to narrow them.
func (s Scope) Allows(repo string) bool {
	if repo == "" {
		return true
	}
	for _, e := range s.Excluded {
		if strings.EqualFold(e, repo) {
			return false
		}
	}
	if len(s.Only) == 0 {
		return true
	}
	for _, o := range s.Only {
		if strings.EqualFold(o, repo) {
			return true
		}
	}
	return false
}

// Filter drops out-of-scope rows, reading the repository from the given key.
func (s Scope) Filter(rows []map[string]any, key string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		name, _ := r[key].(string)
		if s.Allows(name) {
			out = append(out, r)
		}
	}
	return out
}
