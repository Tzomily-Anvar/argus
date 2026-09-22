// Package config is the single place Argus reads configuration.
//
// Every knob resolves through one chain:
//
//	environment variable  >  built-in default
//
// The environment layer is what you set in your configuration file
// (see .env.example for every setting, annotated).
// Nothing here carries a company-specific default: ARGUS_GITHUB_ORG is
// required and has none, so Argus refuses to start rather than silently
// sweeping somewhere you did not intend.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Missing is returned when required configuration is absent. The server
// turns this into a readable startup message rather than a stack trace.
type Missing struct {
	Key  string
	Hint string
}

func (e *Missing) Error() string {
	if e.Hint == "" {
		return fmt.Sprintf("%s is required but not set", e.Key)
	}
	return fmt.Sprintf("%s is required but not set. %s", e.Key, e.Hint)
}

func String(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func Float(key string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func Int(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Bool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// Strings splits a comma-separated value, trimming blanks.
func Strings(key string, def []string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// Org is the GitHub account to sweep. Required, no default.
//
// It takes an organisation or a personal account: which one it is, is
// detected from the name rather than declared, so there is nothing to
// set differently and nobody has to know that GitHub treats the two as
// separate things. The setting keeps its name because changing it would
// break every configuration file already written.
func Org() (string, error) {
	if v := String("ARGUS_GITHUB_ORG", ""); v != "" {
		return v, nil
	}
	return "", &Missing{
		Key:  "ARGUS_GITHUB_ORG",
		Hint: "This is the GitHub organisation, or your own username, to sweep.",
	}
}

// Token is the caller's own GitHub token. Everything Argus shows is
// scoped to it, so two people running Argus see two different dashboards.
// ArgusToken is a token set specifically for Argus, as opposed to one
// that merely happens to be in the environment. The difference decides
// whether it outranks a deliberate `argus login`.
func ArgusToken() string { return String("ARGUS_GITHUB_TOKEN", "") }

// AmbientToken is a token Argus inherited - GH_TOKEN or GITHUB_TOKEN,
// which plenty of people export for other tools entirely.
func AmbientToken() string {
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func Token() (string, error) {
	for _, k := range []string{"ARGUS_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v, nil
		}
	}
	return "", &Missing{
		Key:  "ARGUS_GITHUB_TOKEN",
		Hint: "Run `argus login` to sign in instead, or see the token setup section in the README.",
	}
}

// Repos is an allowlist of repository names. Empty means the whole
// account, which is the default: most people want everything.
func Repos() []string { return Strings("ARGUS_REPOS", nil) }

// ExcludeRepos names repositories to skip. Applied everywhere, so a
// repository left out here is absent from pull request sweeps and alert
// counts alike, rather than only one of them.
func ExcludeRepos() []string { return Strings("ARGUS_EXCLUDE_REPOS", nil) }

// IncludeArchived reports whether archived repositories are swept. They
// are excluded by default: nothing in an archive is actionable.
func IncludeArchived() bool { return Bool("ARGUS_INCLUDE_ARCHIVED", false) }

// GitHubAppClientID enables the GitHub App path. A client id is public -
// the device flow needs no client secret - so it ships as an ordinary
// default rather than a credential.
//
// Set it and Argus signs in as an App user instead of using a personal
// access token, which is what makes read-only enforced by GitHub rather
// than by this code alone.
//
// This default is the published Argus App, so `argus login` works on a
// fresh install with nothing configured. Run your own App instead by
// setting this - the flow is identical, only the App differs.
const publishedAppClientID = "Iv23liJ9UcLoW3YL1foH"

func GitHubAppClientID() string {
	return String("ARGUS_GITHUB_APP_CLIENT_ID", publishedAppClientID)
}

// UseGitHubApp reports whether the App path is configured.
func UseGitHubApp() bool { return GitHubAppClientID() != "" }

func Port() string { return String("ARGUS_PORT", "18474") }

// Bind is the interface to listen on. It defaults to loopback, so running
// the binary directly never exposes your organisation's data to the
// network. The container image overrides it to 0.0.0.0, because Docker
// forwards to the container's own address rather than its loopback - and
// compose publishes that port to 127.0.0.1 only, so the effective
// exposure is the same.
func Bind() string { return String("ARGUS_BIND", "127.0.0.1") }

// Addr is the listen address passed to the HTTP server.
func Addr() string     { return Bind() + ":" + Port() }
func Concurrency() int { return Int("ARGUS_CONCURRENCY", 10) }
func HTTPTimeout() int { return Int("ARGUS_HTTP_TIMEOUT_SECONDS", 45) }

// EnabledTools names the tools that run. The pull request watchdog is the
// default and the only one on unless asked for: everything else costs
// credentials, storage, or both, and nobody should have to opt out of a
// tool they never wanted.
//
//	ARGUS_TOOLS=pr           the default
//	ARGUS_TOOLS=pr,sprint    both
func EnabledTools() []string { return Strings("ARGUS_TOOLS", []string{"pr"}) }

// ToolEnabled reports whether a tool is switched on.
func ToolEnabled(id string) bool {
	for _, t := range EnabledTools() {
		if strings.EqualFold(strings.TrimSpace(t), id) {
			return true
		}
	}
	return false
}

// ---- Jira, for the sprint report ------------------------------------
//
// None of these have a company-specific default. Field ids in particular
// differ per Jira site, so they must be configured or resolved by name -
// a hardcoded customfield_10016 is right for one site and silently wrong
// for the next.

func JiraBaseURL() (string, error) {
	if v := String("ARGUS_JIRA_BASE_URL", ""); v != "" {
		return v, nil
	}
	return "", &Missing{
		Key:  "ARGUS_JIRA_BASE_URL",
		Hint: "Your Jira site, e.g. https://yourcompany.atlassian.net",
	}
}

// JiraCredentials returns the email and API token. Jira Cloud wants Basic
// auth with both, not a bearer token.
func JiraCredentials() (string, string, error) {
	email := String("ARGUS_JIRA_EMAIL", "")
	token := String("ARGUS_JIRA_TOKEN", "")
	if email == "" {
		return "", "", &Missing{
			Key:  "ARGUS_JIRA_EMAIL",
			Hint: "The address you log in to Jira with.",
		}
	}
	if token == "" {
		return "", "", &Missing{
			Key:  "ARGUS_JIRA_TOKEN",
			Hint: "Create one at id.atlassian.com/manage-profile/security/api-tokens",
		}
	}
	return email, token, nil
}

func JiraProject() (string, error) {
	if v := String("ARGUS_JIRA_PROJECT", ""); v != "" {
		return v, nil
	}
	return "", &Missing{
		Key:  "ARGUS_JIRA_PROJECT",
		Hint: "The project key whose sprints you report on, e.g. ENG.",
	}
}

// JiraPointsField is the custom field holding story points. Left empty,
// the tool resolves it by the name in JiraPointsFieldName.
func JiraPointsField() string { return String("ARGUS_JIRA_POINTS_FIELD", "") }

func JiraPointsFieldName() string {
	return String("ARGUS_JIRA_POINTS_FIELD_NAME", "Story Points")
}

// JiraEstimateFieldName is the field holding the planning estimate.
//
// It is a fallback, not a second opinion: a ticket that reaches a Done
// status without its actual points ever being filled in is credited the
// estimate rather than zero, because zero for work that demonstrably
// happened is the further of the two from the truth. Every ticket this
// happens to is named in the report, so the fallback is visible.
func JiraEstimateFieldName() string {
	return String("ARGUS_JIRA_ESTIMATE_FIELD_NAME", "Story point estimate")
}

// JiraStoryLinkTypes are the issue link types that tie a container issue
// to the work beneath it.
//
// Jira's parent field points at the Epic for a team that puts Tasks under
// a Story, so the association has nowhere to live but issue links - and
// which link type carries it is a local habit rather than a standard. A
// team that migrated between projects usually has two.
func JiraStoryLinkTypes() []string {
	return Strings("ARGUS_JIRA_STORY_LINK_TYPES", []string{"Blocks", "migration_parent", "Relates"})
}

func JiraSprintField() string { return String("ARGUS_JIRA_SPRINT_FIELD", "") }

func JiraSprintFieldName() string {
	return String("ARGUS_JIRA_SPRINT_FIELD_NAME", "Sprint")
}

// JiraDoneStatuses are the status names that count as delivered. Matched
// by name, not by Jira's "done" category: teams routinely have several
// statuses in that category and disagree about which mean finished.
func JiraDoneStatuses() []string {
	return Strings("ARGUS_JIRA_DONE_STATUSES", []string{"Done"})
}

// JiraExcludedTypes are issue types left out of the say/do count, usually
// the container types that carry no commitment of their own.
func JiraExcludedTypes() []string {
	return Strings("ARGUS_JIRA_EXCLUDED_TYPES", []string{"Epic"})
}

// JiraSprintLengthDays is the nominal sprint length in working days. It
// is what turns days off into a share of a person's baseline.
func JiraSprintLengthDays() int { return Int("ARGUS_JIRA_SPRINT_LENGTH_DAYS", 10) }

// HoursPerPoint is what one story point means in hours for this team.
//
// Points are not hours, and treating them as a currency is how estimation
// goes wrong. But a team that has agreed "a point is about a day" needs
// that agreement written down somewhere, or a baseline of 10 is a number
// with no units and nobody can tell whether it is right.
func HoursPerPoint() float64 {
	return Float("ARGUS_SPRINT_HOURS_PER_POINT", 6)
}

// HoursPerDay is a working day, used to relate points to days.
func HoursPerDay() float64 {
	return Float("ARGUS_SPRINT_HOURS_PER_DAY", 6)
}

// ---- the Atlassian team, for importing the roster --------------------
//
// Optional, and deliberately so. Without these two the roster is typed in
// by hand, which is all a small team ever needs; with them the people on
// an Atlassian team can be pulled in instead of copying account ids about.
//
// They identify an organisation, so they have no default and none is
// invented here. An unset value means the import is simply not offered.

// AtlassianOrgID is the Atlassian organisation the team belongs to.
func AtlassianOrgID() string { return String("ARGUS_ATLASSIAN_ORG_ID", "") }

// AtlassianTeamID is the team whose members the roster can be imported
// from.
func AtlassianTeamID() string { return String("ARGUS_ATLASSIAN_TEAM_ID", "") }

// AtlassianTeamConfigured reports whether both halves are set. One
// without the other is not half a configuration, it is a mistake, and the
// import stays hidden rather than failing at the moment it is pressed.
func AtlassianTeamConfigured() bool {
	return AtlassianOrgID() != "" && AtlassianTeamID() != ""
}

// SprintWritesAllowed gates every write the sprint tool can make.
// Off by default: a tool that can reassign tickets and publish pages
// should be something you switch on deliberately.
func SprintWritesAllowed() bool { return Bool("ARGUS_SPRINT_ALLOW_WRITES", false) }
