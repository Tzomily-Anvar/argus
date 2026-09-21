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

func Port() string     { return String("ARGUS_PORT", "18474") }
func Concurrency() int { return Int("ARGUS_CONCURRENCY", 10) }
func HTTPTimeout() int { return Int("ARGUS_HTTP_TIMEOUT_SECONDS", 45) }
