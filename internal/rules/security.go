package rules

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

func init() {
	Register(Rule{
		ID:          "security",
		Title:       "Security alerts",
		Description: "Open Dependabot and code-scanning alerts across the account, plus outstanding Dependabot pull requests.",
		Why: "Alert counts alone are noise. This separates first-party findings from vendored dependencies so the " +
			"critical ones are actually visible. Needs a token that can read security alerts. On a personal " +
			"account there is no account-wide alert feed, so the alerts are read one repository at a time - " +
			"the same answer, but a slower sweep.",
		Enabled: true,
		Params: []Param{
			{
				Name: "noise_pattern",
				Desc: "Regular expression matching dependency paths to discount, e.g. vendored code. " +
					"Matching alerts still appear in totals but not in the per-repository critical rollup.",
				Default: `\.venv|site-packages|node_modules|vendor/`,
			},
			{
				Name: "exclude_repos",
				Desc: "Repositories to leave out of alert counts, comma separated. Useful for archives, " +
					"sandboxes or template repositories whose alerts you will never act on. This is in " +
					"addition to ARGUS_EXCLUDE_REPOS, which applies everywhere.",
				Default: []string{},
			},
			{Name: "include_code_scanning", Desc: "Include code-scanning alerts. Turn off if you do not use them.", Default: true},
			{Name: "include_dependabot_prs", Desc: "Include a count of open Dependabot pull requests.", Default: true},
		},
		Run: runSecurity,
	})
}

// runSecurity fans its three independent checks out concurrently, and
// keeps each one's failure to itself: a token without security_events
// scope should cost you the alert counts, not the whole section.
func runSecurity(c *Context, v Values) (any, error) {
	noise, err := regexp.Compile(v.Str("noise_pattern"))
	if err != nil {
		return nil, err
	}

	// The account-wide alert endpoints cannot be narrowed, so scoping
	// happens here. This rule's own exclusions stack on the global ones.
	scope := c.Scope
	scope.Excluded = append(append([]string{}, scope.Excluded...), v.Strs("exclude_repos")...)

	type check struct {
		key string
		fn  func() (map[string]any, error)
	}
	checks := []check{{"dependabot", func() (map[string]any, error) { return dependabotAlerts(c, noise, scope) }}}
	if v.Bool("include_code_scanning") {
		checks = append(checks, check{"code_scanning", func() (map[string]any, error) { return codeScanningAlerts(c, scope) }})
	}
	if v.Bool("include_dependabot_prs") {
		checks = append(checks, check{"dependabot_prs", func() (map[string]any, error) { return dependabotPRs(c) }})
	}

	type result struct {
		key  string
		data map[string]any
	}
	results := gh.PMap(checks, len(checks), func(ch check) result {
		data, err := ch.fn()
		if err != nil {
			return result{ch.key, map[string]any{"error": err.Error()}}
		}
		return result{ch.key, data}
	})

	out := map[string]any{}
	for _, r := range results {
		out[r.key] = r.data
	}
	return out, nil
}

// accountAlerts fetches one of GitHub's security alert feeds.
//
// GitHub publishes these account-wide for an organisation only. There is
// no /users/{login}/dependabot/alerts and no personal equivalent of it,
// and the same is true of code scanning - the only other place either
// feed exists is per repository. So on a personal account the alerts are
// collected a repository at a time and stitched back together. The rows
// are identical; the request count is not, which is why this is not the
// path taken when the account-wide feed exists.
//
// A repository with the feature switched off answers 403 or 404. That is
// ordinary and is skipped. Every repository failing is not ordinary, and
// is returned as an error rather than as an empty list, because empty
// reads as "nothing to fix" and that is the wrong thing to believe about
// your own security alerts.
func accountAlerts(c *Context, feed string, params url.Values) ([]any, error) {
	if !c.Personal() {
		return c.Client.GetAll("/orgs/"+c.Org+"/"+feed, params)
	}

	repos, err := c.Repos()
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}

	type batch struct {
		alerts []any
		err    error
	}
	got := gh.PMap(repos, c.Concurrency, func(r map[string]any) batch {
		name := gh.Str(r["name"])
		alerts, err := c.Client.GetAll("/repos/"+c.Org+"/"+name+"/"+feed, params)
		if err != nil {
			return batch{err: err}
		}
		// The per-repository feed leaves out which repository the alert
		// belongs to, because the path already said. The account-wide
		// feed includes it and everything downstream reads it from
		// there, so put it back rather than teach every caller both
		// shapes.
		for _, a := range alerts {
			if m, ok := a.(map[string]any); ok {
				m["repository"] = map[string]any{"name": name}
			}
		}
		return batch{alerts: alerts}
	})

	var out []any
	var firstErr error
	errs := make([]error, 0, len(got))
	refused := 0
	for _, b := range got {
		errs = append(errs, b.err)
		if b.err != nil {
			refused++
			if firstErr == nil {
				firstErr = b.err
			}
			continue
		}
		out = append(out, b.alerts...)
	}
	// A repository that refused because the feature is off is ordinary
	// and is skipped. One that refused because the budget ran out is not:
	// skipping it turns "we did not look" into "nothing was found", which
	// is the wrong thing to believe about your own security alerts.
	if err := firstRateLimit(errs); err != nil {
		return nil, err
	}
	if refused == len(repos) {
		return nil, fmt.Errorf(
			"could not read %s for any of the %d repositories in scope. On a personal account these "+
				"are read per repository, and every one refused - either the feature is off "+
				"everywhere, or the token cannot read security alerts. Last response: %v",
			feed, len(repos), firstErr)
	}
	return out, nil
}

func dependabotAlerts(c *Context, noise *regexp.Regexp, scope Scope) (map[string]any, error) {
	alerts, err := accountAlerts(c, "dependabot/alerts", Params("state", "open"))
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}
	byRepo := map[string]map[string]int{}
	var criticals []map[string]any

	for _, a := range gh.Maps(alerts) {
		if !scope.Allows(gh.Str(gh.Map(a["repository"])["name"])) {
			continue
		}
		vuln := gh.Map(a["security_vulnerability"])
		sev := gh.Str(vuln["severity"])
		if sev == "" {
			sev = "?"
		}
		counts[sev]++

		if sev != "critical" && sev != "high" {
			continue
		}
		dep := gh.Map(a["dependency"])
		path := gh.Str(dep["manifest_path"])
		if noise.MatchString(path) {
			// Counted in the totals above, but kept out of the rollup:
			// a vendored lockfile should not outrank first-party code.
			continue
		}

		repo := gh.Str(gh.Map(a["repository"])["name"])
		if byRepo[repo] == nil {
			byRepo[repo] = map[string]int{"critical": 0, "high": 0}
		}
		byRepo[repo][sev]++

		if sev == "critical" {
			advisory := gh.Map(a["security_advisory"])
			cve := gh.Str(advisory["cve_id"])
			if cve == "" {
				cve = gh.Str(advisory["ghsa_id"])
			}
			criticals = append(criticals, map[string]any{
				"repo":     repo,
				"package":  gh.Str(gh.Map(vuln["package"])["name"]),
				"scope":    gh.Str(dep["scope"]),
				"cve":      cve,
				"manifest": path,
				"url":      gh.Str(a["html_url"]),
			})
		}
	}

	return map[string]any{
		"counts":            counts,
		"crit_high_by_repo": rankRepos(byRepo),
		"criticals":         Rows(criticals),
	}, nil
}

// rankRepos orders repositories worst-first, weighting criticals far
// above highs so one critical always outranks a pile of highs.
func rankRepos(byRepo map[string]map[string]int) []map[string]any {
	out := make([]map[string]any, 0, len(byRepo))
	for repo, sev := range byRepo {
		out = append(out, map[string]any{
			"repo": repo, "critical": sev["critical"], "high": sev["high"],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		score := func(m map[string]any) int { return m["critical"].(int)*1000 + m["high"].(int) }
		if score(out[i]) != score(out[j]) {
			return score(out[i]) > score(out[j])
		}
		return out[i]["repo"].(string) < out[j]["repo"].(string)
	})
	return out
}

func codeScanningAlerts(c *Context, scope Scope) (map[string]any, error) {
	alerts, err := accountAlerts(c, "code-scanning/alerts", Params("state", "open"))
	if err != nil {
		return nil, err
	}

	byRepo := map[string]map[string]int{}
	var critHigh []map[string]any
	total := 0

	for _, a := range gh.Maps(alerts) {
		repo := gh.Str(gh.Map(a["repository"])["name"])
		if !scope.Allows(repo) {
			continue
		}
		rule := gh.Map(a["rule"])
		sev := gh.Str(rule["security_severity_level"])
		if sev == "" {
			sev = gh.Str(rule["severity"])
		}
		if sev == "" {
			sev = "?"
		}
		if byRepo[repo] == nil {
			byRepo[repo] = map[string]int{}
		}
		byRepo[repo][sev]++
		total++

		if sev == "critical" || sev == "high" {
			name := gh.Str(rule["name"])
			if name == "" {
				name = gh.Str(rule["id"])
			}
			critHigh = append(critHigh, map[string]any{
				"repo": repo, "severity": sev, "rule": name, "url": gh.Str(a["html_url"]),
			})
		}
	}

	sort.SliceStable(critHigh, func(i, j int) bool {
		ci := critHigh[i]["severity"] == "critical"
		cj := critHigh[j]["severity"] == "critical"
		if ci != cj {
			return ci
		}
		return critHigh[i]["repo"].(string) < critHigh[j]["repo"].(string)
	})

	counts := map[string]any{}
	for repo, sevs := range byRepo {
		m := map[string]any{}
		for sev, n := range sevs {
			m[sev] = n
		}
		counts[repo] = m
	}
	return map[string]any{"by_repo": counts, "crit_high": Rows(critHigh), "total": total}, nil
}

const dependabotAuthor = "app/dependabot"

func dependabotPRs(c *Context) (map[string]any, error) {
	// Dependabot is the noisiest author on most accounts - a hundred open
	// pull requests here - so this was two of the sweep's search requests
	// on its own, for a set the shared listing already holds.
	items, err := c.OpenPRsWhere("is:pr is:open author:"+dependabotAuthor, func(it map[string]any) bool {
		return MatchesAuthor(it, dependabotAuthor)
	})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	total := 0
	for _, it := range items {
		counts[RepoName(it)]++
		total++
	}
	type kv struct {
		repo string
		n    int
	}
	ordered := make([]kv, 0, len(counts))
	for repo, n := range counts {
		ordered = append(ordered, kv{repo, n})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].n != ordered[j].n {
			return ordered[i].n > ordered[j].n
		}
		return ordered[i].repo < ordered[j].repo
	})
	byRepo := make([]map[string]any, 0, len(ordered))
	for _, o := range ordered {
		byRepo = append(byRepo, map[string]any{"repo": o.repo, "count": o.n})
	}
	return map[string]any{"total": total, "by_repo": byRepo}, nil
}
