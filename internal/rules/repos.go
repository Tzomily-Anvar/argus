package rules

import (
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Listing an account's repositories.
//
// There is no single endpoint that works for both kinds of account, and
// the ones that exist are not interchangeable:
//
//   - /orgs/{org}/repos lists an organisation's repositories, including
//     the private ones the token can see. It answers 404 for a personal
//     account, which is what sent anyone pointing Argus at their own
//     profile into an empty dashboard.
//
//   - /user/repos is resolved against the caller rather than against a
//     name, so it is the only listing that includes a person's private
//     repositories. It therefore only works for the account you are
//     signed in as.
//
//   - /users/{login}/repos works for anyone, and returns public
//     repositories only. It is the fallback for a personal account that
//     is not yours - a limit of the API, not of the token.
//
// The choice is made once here so that no rule has to make it, and the
// result is shared across the sweep.

// Repos returns the repositories in scope, listed at most once per
// sweep. Rules run concurrently, so the first caller does the work and
// the rest wait for it.
func (c *Context) Repos() ([]map[string]any, error) {
	c.reposOnce.Do(func() { c.repos, c.reposErr = c.listRepos() })
	return c.repos, c.reposErr
}

func (c *Context) listRepos() ([]map[string]any, error) {
	path, params := "/orgs/"+c.Org+"/repos", Params("type", "all", "sort", "pushed")
	if c.Personal() {
		if strings.EqualFold(c.Org, c.Me) {
			// affiliation and type cannot both be given - GitHub rejects
			// the pair with a 422 - and affiliation is the one that
			// brings back private repositories.
			path, params = "/user/repos", Params("affiliation", "owner", "sort", "pushed")
		} else {
			path, params = "/users/"+c.Org+"/repos", Params("type", "owner", "sort", "pushed")
		}
	}

	raw, err := c.Client.GetAll(path, params)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(raw))
	for _, r := range gh.Maps(raw) {
		if gh.Bool(r["archived"]) && !c.Scope.Archived {
			continue
		}
		if !c.Scope.Allows(gh.Str(r["name"])) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
