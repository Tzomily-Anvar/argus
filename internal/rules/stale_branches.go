package rules

import (
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

const branchQuery = `
query($owner:String!,$name:String!){ repository(owner:$owner,name:$name){
  defaultBranchRef{name}
  refs(refPrefix:"refs/heads/",first:100,orderBy:{field:TAG_COMMIT_DATE,direction:ASC}){
    nodes{ name target{... on Commit{committedDate author{user{login}}}} }
  }}}`

func init() {
	Register(Rule{
		ID:          "stale_branches",
		Title:       "Stale branches",
		Description: "Branches with no commit in a long time, excluding each repository's default branch.",
		Why:         "Abandoned branches accumulate quietly and make a repository harder to navigate. Yours are flagged so you can delete them.",
		Enabled:     true,
		Params: []Param{
			{Name: "days", Desc: "A branch with no commit in this many days counts as stale.", Default: 60},
			{Name: "scan_days", Desc: "Only scan repositories pushed to within this many days. Keeps the sweep proportional to active work.", Default: 180},
			{Name: "ignore_prefixes", Desc: "Branch name prefixes to skip, comma separated.", Default: []string{"dependabot/"}},
		},
		Run: runStaleBranches,
	})
}

func runStaleBranches(c *Context, v Values) (any, error) {
	pushCut := c.Now.AddDate(0, 0, -v.Int("scan_days"))
	branchCut := c.Now.AddDate(0, 0, -v.Int("days"))
	skip := v.Strs("ignore_prefixes")

	reposRaw, err := c.Client.GetAll("/orgs/"+c.Org+"/repos", Params("type", "all", "sort", "pushed"))
	if err != nil {
		return nil, err
	}

	var repos []map[string]any
	for _, r := range gh.Maps(reposRaw) {
		if gh.Bool(r["archived"]) {
			continue
		}
		pushed, err := time.Parse(time.RFC3339, gh.Str(r["pushed_at"]))
		if err != nil || !pushed.After(pushCut) {
			continue
		}
		repos = append(repos, r)
	}

	// One GraphQL call per repository, fanned out. This is the rule that
	// benefits most from real parallelism.
	found := gh.PMap(repos, c.Concurrency, func(r map[string]any) []map[string]any {
		name := gh.Str(r["name"])
		data, err := c.Client.GraphQL(branchQuery, map[string]any{"owner": c.Org, "name": name})
		if err != nil {
			return nil
		}
		repo := gh.Map(data["repository"])
		defaultBranch := gh.Str(gh.Map(repo["defaultBranchRef"])["name"])

		var out []map[string]any
		for _, raw := range gh.List(gh.Map(repo["refs"])["nodes"]) {
			node := gh.Map(raw)
			branch := gh.Str(node["name"])
			target := gh.Map(node["target"])
			date := gh.Str(target["committedDate"])
			if date == "" || branch == defaultBranch || hasAnyPrefix(branch, skip) {
				continue
			}
			t, err := time.Parse(time.RFC3339, date)
			if err != nil || !t.Before(branchCut) {
				continue
			}
			login := gh.Str(gh.Map(gh.Map(target["author"])["user"])["login"])
			if login == "" {
				login = "?"
			}
			out = append(out, map[string]any{
				"repo":        name,
				"branch":      branch,
				"last_commit": day(date),
				"author":      login,
				"mine":        login == c.Me,
				"age_days":    DaysSince(date, c.Now),
				"url":         "https://github.com/" + c.Org + "/" + name + "/tree/" + branch,
			})
		}
		return out
	})

	var stale []map[string]any
	for _, batch := range found {
		stale = append(stale, batch...)
	}
	return SortByAge(stale), nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
