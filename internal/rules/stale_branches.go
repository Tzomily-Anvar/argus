package rules

import (
	"strconv"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Repositories per GraphQL document.
//
// One query per repository meant twenty-nine round trips on a modest
// account, and GraphQL charges by the nodes a query walks rather than by
// the number of queries - so asking about ten repositories at once costs
// roughly what one of them did, ten times less often. Ten is a
// compromise: larger documents keep saving requests but take long enough
// to answer that a single slow repository stalls a bigger batch.
const branchBatch = 10

// branchFragment is the per-repository selection, written once and
// aliased. It follows the query rather than preceding it because the
// read-only guard insists a document begins with the word query, and a
// document that opened with its fragment would be refused - correctly,
// since the guard cannot be asked to parse GraphQL to find out.
const branchFragment = `
fragment branches on Repository{
  defaultBranchRef{name}
  refs(refPrefix:"refs/heads/",first:100,orderBy:{field:TAG_COMMIT_DATE,direction:ASC}){
    nodes{ name target{... on Commit{committedDate author{user{login}}}} }
  }
}`

// branchQueryFor builds one document covering several repositories, with
// each name passed as a variable rather than interpolated - a repository
// name is not ours to trust into a query string.
func branchQueryFor(names []string) (string, map[string]any, []string) {
	var params, body strings.Builder
	vars := map[string]any{}
	aliases := make([]string, len(names))
	for i, name := range names {
		v := "n" + strconv.Itoa(i)
		aliases[i] = "r" + strconv.Itoa(i)
		params.WriteString(",$" + v + ":String!")
		body.WriteString("  " + aliases[i] + ": repository(owner:$owner,name:$" + v + "){...branches}\n")
		vars[v] = name
	}
	vars["owner"] = ""
	return "query($owner:String!" + params.String() + "){\n" + body.String() + "}" + branchFragment, vars, aliases
}

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

	// Scope and archived repositories are already accounted for; what is
	// left is this rule's own "has anything happened here lately" filter.
	inScope, err := c.Repos()
	if err != nil {
		return nil, err
	}

	var repos []map[string]any
	for _, r := range inScope {
		pushed, err := time.Parse(time.RFC3339, gh.Str(r["pushed_at"]))
		if err != nil || !pushed.After(pushCut) {
			continue
		}
		repos = append(repos, r)
	}

	// Batched, then fanned out. This is the rule that benefits most from
	// real parallelism, and the one that made the most requests.
	var batches [][]string
	for i := 0; i < len(repos); i += branchBatch {
		end := min(i+branchBatch, len(repos))
		names := make([]string, 0, end-i)
		for _, r := range repos[i:end] {
			names = append(names, gh.Str(r["name"]))
		}
		batches = append(batches, names)
	}

	type outcome struct {
		rows []map[string]any
		err  error
	}
	found := gh.PMap(batches, c.Concurrency, func(names []string) outcome {
		query, vars, aliases := branchQueryFor(names)
		vars["owner"] = c.Org
		data, err := c.Client.GraphQL(query, vars)
		// A repository the token cannot see comes back as an error beside
		// the ones it can, so partial data is the normal case for a batch
		// and is kept.
		if err != nil && !gh.IsPartial(err) {
			return outcome{err: err}
		}

		var out []map[string]any
		for i, alias := range aliases {
			out = append(out, staleIn(c, gh.Map(data[alias]), names[i], branchCut, skip)...)
		}
		return outcome{rows: out}
	})

	var stale []map[string]any
	errs := make([]error, 0, len(found))
	for _, batch := range found {
		errs = append(errs, batch.err)
		stale = append(stale, batch.rows...)
	}
	// Half a branch listing is not a shorter answer, it is a wrong one.
	if err := firstRateLimit(errs); err != nil {
		return nil, err
	}
	return SortByAge(stale), nil
}

// staleIn picks the stale branches out of one repository's node.
func staleIn(c *Context, repo map[string]any, name string, cut time.Time, skip []string) []map[string]any {
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
		if err != nil || !t.Before(cut) {
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
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
