package rules

import (
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Context is what every rule is handed: an authenticated client plus the
// few facts each one needs about who is asking. Built once per sweep.
type Context struct {
	Client *gh.Client

	// Org is the account being swept. It is an organisation login or a
	// personal one, and Account says which - several endpoints exist for
	// only one of the two. The field keeps its name because the
	// snapshot, the API and the dashboard all already call it that.
	Org     string
	Account gh.AccountKind

	Me          string
	Teams       []string
	Now         time.Time
	Concurrency int
	Scope       Scope

	// The account's repositories, listed at most once per sweep. Two
	// rules want the same list and they run concurrently, so the work is
	// shared rather than done twice.
	reposOnce sync.Once
	repos     []map[string]any
	reposErr  error

	// Every open pull request in scope, searched at most once per sweep.
	// Four rules each used to run their own search for a subset of this
	// one list, and search is the tightest budget GitHub grants.
	openPRsOnce sync.Once
	openPRs     []map[string]any
	openPRsErr  error
	openPRsFull bool

	// One fetch per pull request per sweep, however many rules ask. See
	// pullRequest below.
	prMu    sync.Mutex
	prNodes map[string]*prNode
}

// Personal reports whether the configured account is somebody's own
// rather than an organisation.
func (c *Context) Personal() bool { return c.Account.Personal() }

// NewContext resolves which kind of account is configured, the viewer's
// identity, and their team memberships. Two or three small GETs, well
// under a second.
func NewContext(client *gh.Client) (*Context, error) {
	account, err := config.Org()
	if err != nil {
		return nil, err
	}

	// First, because everything after it depends on the answer, and
	// because a name GitHub does not recognise is worth failing on here
	// with a plain message rather than as eight separate rule errors.
	kind, err := client.AccountKindOf(account)
	if err != nil {
		return nil, err
	}

	user, err := client.Get("/user", nil)
	if err != nil {
		return nil, err
	}

	// Teams belong to organisations. On a personal account the filter
	// below could never match, so the request is not made at all: it is
	// one fewer permission the token has to carry for a result that is
	// empty by definition.
	var teams []string
	if !kind.Personal() {
		teamsRaw, err := client.GetAll("/user/teams", nil)
		if err != nil {
			return nil, err
		}
		for _, t := range gh.Maps(teamsRaw) {
			if gh.Str(gh.Map(t["organization"])["login"]) == account {
				if slug := gh.Str(t["slug"]); slug != "" {
					teams = append(teams, slug)
				}
			}
		}
	}

	return &Context{
		Client:      client,
		Org:         account,
		Account:     kind,
		Me:          gh.Str(user["login"]),
		Teams:       teams,
		Now:         time.Now().UTC(),
		Concurrency: config.Concurrency(),
		Scope: Scope{
			Org:      account,
			Personal: kind.Personal(),
			Only:     config.Repos(),
			Excluded: config.ExcludeRepos(),
			Archived: config.IncludeArchived(),
		},
	}, nil
}

// Search runs an issue search scoped to the configured repositories.
func (c *Context) Search(query string) ([]map[string]any, error) {
	items, _, err := c.Client.SearchIssues(c.Scope.Query() + " " + query)
	if err != nil {
		return nil, err
	}
	return gh.Maps(items), nil
}

// ---- row helpers -----------------------------------------------------

// RepoName pulls the short repository name out of an issue/PR payload.
func RepoName(item map[string]any) string {
	parts := strings.Split(gh.Str(item["repository_url"]), "/")
	return parts[len(parts)-1]
}

// DaysSince returns whole days between an RFC3339 timestamp and now.
func DaysSince(iso string, now time.Time) int {
	if iso == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return 0
	}
	return int(now.Sub(t).Hours() / 24)
}

// Row builds the common shape every PR-shaped result shares, so the
// frontend can render any rule's rows with one component.
func Row(item map[string]any, now time.Time, extra map[string]any) map[string]any {
	created := gh.Str(item["created_at"])
	author := gh.Str(gh.Map(item["user"])["login"])
	if author == "" {
		author = "?"
	}
	row := map[string]any{
		"repo":     RepoName(item),
		"number":   item["number"],
		"title":    gh.Str(item["title"]),
		"author":   author,
		"created":  day(created),
		"updated":  day(gh.Str(item["updated_at"])),
		"age_days": DaysSince(created, now),
		"url":      gh.Str(item["html_url"]),
	}
	for k, v := range extra {
		row[k] = v
	}
	return row
}

func day(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// Number returns a PR number as an int.
func Number(item map[string]any) int { return int(gh.Num(item["number"])) }

// NumberStr returns a PR number as a string, for URL building.
func NumberStr(item map[string]any) string { return strconv.Itoa(Number(item)) }

// IsBot reports whether a login belongs to a bot account.
func IsBot(login string) bool { return strings.Contains(login, "[bot]") }

// AuthorLogin returns an item's author login.
func AuthorLogin(item map[string]any) string {
	return gh.Str(gh.Map(item["user"])["login"])
}

// Labels returns an item's label names.
func Labels(item map[string]any) []string {
	var out []string
	for _, l := range gh.List(item["labels"]) {
		if name := gh.Str(gh.Map(l)["name"]); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// Rows guards against Go encoding a nil slice as JSON null. An empty
// result and a missing result are the same thing to a reader, but not to
// a frontend calling .length on it, so every rule returns [] not null.
func Rows(rows []map[string]any) []map[string]any {
	if rows == nil {
		return []map[string]any{}
	}
	return rows
}

// SortByAge orders rows oldest first, which is the order that matters for
// everything Argus surfaces.
func SortByAge(rows []map[string]any) []map[string]any {
	sortStable(rows, func(a, b map[string]any) bool {
		ai, _ := a["age_days"].(int)
		bi, _ := b["age_days"].(int)
		return ai > bi
	})
	return Rows(rows)
}

// Compact drops nil entries left by a PMap whose worker returned nothing.
func Compact(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// Params builds a url.Values from alternating key/value pairs.
func Params(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// ---- shared work ------------------------------------------------------

// OpenPRs returns every open pull request in scope, searched at most once
// per sweep, and whether that listing is complete.
//
// Four rules - the inventory, your own pull requests, the stale ones and
// the Dependabot count - each ran a search for a different subset of the
// same set. Search is metered at thirty requests a minute against five
// thousand an hour for everything else, so those were the four most
// expensive requests in the sweep and three of them were redundant.
//
// The completeness flag is not decoration. Search stops at a thousand
// results however many match, so on a busy account one broad query can be
// truncated where the narrow ones would not have been. When that happens
// this listing is not a valid stand-in for anything and the caller must
// run its own query - the sweep costs more, and reports the truth.
func (c *Context) OpenPRs() ([]map[string]any, bool, error) {
	c.openPRsOnce.Do(func() {
		items, total, err := c.Client.SearchIssues(c.Scope.Query() + " is:pr is:open")
		if err != nil {
			c.openPRsErr = err
			return
		}
		c.openPRs = gh.Maps(items)
		c.openPRsFull = total <= gh.SearchCeiling
	})
	return c.openPRs, c.openPRsFull, c.openPRsErr
}

// OpenPRsWhere returns the open pull requests matching a predicate, or
// falls back to running `query` as its own search when the shared listing
// cannot be trusted. Either way the caller gets the same set.
func (c *Context) OpenPRsWhere(query string, keep func(map[string]any) bool) ([]map[string]any, error) {
	all, complete, err := c.OpenPRs()
	if err != nil {
		return nil, err
	}
	if !complete {
		return c.Search(query)
	}
	out := make([]map[string]any, 0, len(all))
	for _, it := range all {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out, nil
}

// MatchesAuthor applies search's author: qualifier to an item that has
// already been fetched.
//
// GitHub spells an app's authorship two ways: the search qualifier wants
// app/dependabot, and the result it hands back carries the login
// dependabot[bot]. Filtering locally means speaking both, so the one
// place that translates between them is here rather than in each rule.
func MatchesAuthor(item map[string]any, qualifier string) bool {
	login := AuthorLogin(item)
	if app, ok := strings.CutPrefix(qualifier, "app/"); ok {
		return strings.EqualFold(login, app+"[bot]")
	}
	return strings.EqualFold(login, qualifier)
}

// prNode is one pull request's detail, fetched once however many rules
// want it.
type prNode struct {
	once sync.Once
	data map[string]any
	err  error
}

// PullRequest returns a pull request's review state, labels and checks,
// fetched at most once per sweep.
//
// Three rules want the same pull request. The inventory wants its review
// decision and check results; your own pull requests want the same for
// the ones you wrote; the unreviewed rule wants to know whether anyone
// has been asked. They ran concurrently and each fetched it separately,
// and the unreviewed rule fetched it over REST for a single field that
// the other two were already getting.
//
// The raw node is shared rather than the interpretation of it, because
// the interpretation depends on each rule's own configured policy gates.
func (c *Context) PullRequest(repo string, number int) (map[string]any, error) {
	key := repo + "#" + strconv.Itoa(number)

	c.prMu.Lock()
	if c.prNodes == nil {
		c.prNodes = map[string]*prNode{}
	}
	n, ok := c.prNodes[key]
	if !ok {
		n = &prNode{}
		c.prNodes[key] = n
	}
	c.prMu.Unlock()

	n.once.Do(func() {
		data, err := c.Client.GraphQL(pullRequestQuery, map[string]any{
			"owner": c.Org, "name": repo, "number": number,
		})
		// Partial data is still worth having: the review decision and
		// labels usually resolve even when the check contexts do not.
		if err != nil && !gh.IsPartial(err) {
			n.err = err
			return
		}
		n.data = gh.Map(gh.Map(data["repository"])["pullRequest"])
		if n.data == nil {
			n.err = err
		}
	})
	return n.data, n.err
}
