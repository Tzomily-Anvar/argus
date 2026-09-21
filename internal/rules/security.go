package rules

import (
	"regexp"
	"sort"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

func init() {
	Register(Rule{
		ID:          "security",
		Title:       "Security alerts",
		Description: "Open Dependabot and code-scanning alerts across the organisation, plus outstanding Dependabot pull requests.",
		Why: "Alert counts alone are noise. This separates first-party findings from vendored dependencies so the " +
			"critical ones are actually visible. Needs a token that can read security alerts.",
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
			{Name: "include_code_scanning", Desc: "Include code-scanning alerts. Turn off if the organisation does not use them.", Default: true},
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

	// The org-wide alert endpoints cannot be narrowed, so scoping happens
	// here. This rule's own exclusions stack on top of the global ones.
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

func dependabotAlerts(c *Context, noise *regexp.Regexp, scope Scope) (map[string]any, error) {
	alerts, err := c.Client.GetAll("/orgs/"+c.Org+"/dependabot/alerts", Params("state", "open"))
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
	alerts, err := c.Client.GetAll("/orgs/"+c.Org+"/code-scanning/alerts", Params("state", "open"))
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

func dependabotPRs(c *Context) (map[string]any, error) {
	items, err := c.Search("is:pr is:open author:app/dependabot")
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
