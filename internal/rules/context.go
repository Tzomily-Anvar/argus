package rules

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Context is what every rule is handed: an authenticated client plus the
// few facts each one needs about who is asking. Built once per sweep.
type Context struct {
	Client      *gh.Client
	Org         string
	Me          string
	Teams       []string
	Now         time.Time
	Concurrency int
	Scope       Scope
}

// NewContext resolves the viewer's identity and team memberships. This is
// one GET plus one paginated GET, well under a second.
func NewContext(client *gh.Client) (*Context, error) {
	org, err := config.Org()
	if err != nil {
		return nil, err
	}
	user, err := client.Get("/user", nil)
	if err != nil {
		return nil, err
	}
	teamsRaw, err := client.GetAll("/user/teams", nil)
	if err != nil {
		return nil, err
	}
	var teams []string
	for _, t := range gh.Maps(teamsRaw) {
		if gh.Str(gh.Map(t["organization"])["login"]) == org {
			if slug := gh.Str(t["slug"]); slug != "" {
				teams = append(teams, slug)
			}
		}
	}
	return &Context{
		Client:      client,
		Org:         org,
		Me:          gh.Str(user["login"]),
		Teams:       teams,
		Now:         time.Now().UTC(),
		Concurrency: config.Concurrency(),
		Scope: Scope{
			Org:      org,
			Only:     config.Repos(),
			Excluded: config.ExcludeRepos(),
			Archived: config.IncludeArchived(),
		},
	}, nil
}

// Search runs an issue search scoped to the configured repositories.
func (c *Context) Search(query string) ([]map[string]any, error) {
	items, err := c.Client.SearchIssues(c.Scope.Query() + " " + query)
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
