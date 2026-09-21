// Package config is the single place Argus reads configuration.
//
// Every knob resolves through one chain:
//
//	environment variable  >  built-in default
//
// The environment layer is what you set in .env (see .env.example).
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

// Org is the GitHub organisation to sweep. Required, no default.
func Org() (string, error) {
	if v := String("ARGUS_GITHUB_ORG", ""); v != "" {
		return v, nil
	}
	return "", &Missing{
		Key:  "ARGUS_GITHUB_ORG",
		Hint: "Set it in your .env - this is the GitHub organisation to sweep.",
	}
}

// Token is the caller's own GitHub token. Everything Argus shows is
// scoped to it, so two people running Argus see two different dashboards.
func Token() (string, error) {
	for _, k := range []string{"ARGUS_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v, nil
		}
	}
	return "", &Missing{
		Key:  "ARGUS_GITHUB_TOKEN",
		Hint: "See the token setup section in the README, or run `gh auth login` and let run.sh pass it through.",
	}
}

// Repos is an allowlist of repository names. Empty means the whole
// organisation, which is the default: most people want everything.
func Repos() []string { return Strings("ARGUS_REPOS", nil) }

// ExcludeRepos names repositories to skip. Applied everywhere, so a
// repository left out here is absent from pull request sweeps and alert
// counts alike, rather than only one of them.
func ExcludeRepos() []string { return Strings("ARGUS_EXCLUDE_REPOS", nil) }

// IncludeArchived reports whether archived repositories are swept. They
// are excluded by default: nothing in an archive is actionable.
func IncludeArchived() bool { return Bool("ARGUS_INCLUDE_ARCHIVED", false) }

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

// JiraSprintLengthDays is the nominal sprint length, used to turn days
// off into a share of a person's baseline.
func JiraSprintLengthDays() int { return Int("ARGUS_JIRA_SPRINT_LENGTH_DAYS", 10) }

// SprintWritesAllowed gates every write the sprint tool can make.
// Off by default: a tool that can reassign tickets and publish pages
// should be something you switch on deliberately.
func SprintWritesAllowed() bool { return Bool("ARGUS_SPRINT_ALLOW_WRITES", false) }
