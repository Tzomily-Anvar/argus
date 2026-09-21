package sprint

import "net/url"

func urlEscape(s string) string { return url.QueryEscape(s) }

// browseURL is the one way a Jira issue link is built. It returns empty
// rather than a half-formed href when either half is missing, so a
// caller with no base URL configured renders plain text instead of a
// link that goes nowhere.
func browseURL(base, key string) string {
	if base == "" || key == "" {
		return ""
	}
	return base + "/browse/" + key
}
